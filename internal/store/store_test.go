package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestUpsertIsIdempotent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	it := Item{Source: "twitter", SourceID: "42", Kind: "like", Text: "hello lambda"}
	if _, err := s.Upsert([]Item{it}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]Item{it}); err != nil { // same key again
		t.Fatal(err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 row after re-upsert, got %d", n)
	}
}

func TestVecRoundTrip(t *testing.T) {
	in := []float32{0.1, -2, 3.5}
	out := decodeVec(encodeVec(in))
	if len(out) != len(in) {
		t.Fatalf("len %d != %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("at %d: %v != %v", i, out[i], in[i])
		}
	}
}

func TestForeignKeysCascade(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Insert an item.
	it := Item{Source: "test", SourceID: "123", Kind: "doc", Text: "test item"}
	if _, err := s.Upsert([]Item{it}); err != nil {
		t.Fatal(err)
	}

	// Get the item's id.
	var itemID int64
	if err := s.db.QueryRow(`SELECT id FROM items WHERE source = ? AND source_id = ?`, it.Source, it.SourceID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}

	// Insert an embedding for this item.
	vec := encodeVec([]float32{0.1, 0.2, 0.3})
	if _, err := s.db.Exec(`INSERT INTO embeddings (item_id, model, vector) VALUES (?, ?, ?)`, itemID, "test-model", vec); err != nil {
		t.Fatal(err)
	}

	// Delete the item; cascade should delete its embeddings.
	if _, err := s.db.Exec(`DELETE FROM items WHERE id = ?`, itemID); err != nil {
		t.Fatal(err)
	}

	// Verify no embeddings remain.
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM embeddings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 embeddings after cascade delete, got %d", count)
	}
}

func TestOpenCreatesParentDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "share", "glane", "glane.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

func TestOpenReadOnlyNeverWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "glane.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start on lambda", URL: "https://x/1"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	before, _ := os.ReadFile(p)

	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ro.SearchFTS("cold", Filter{})
	if err != nil || len(res) != 1 {
		t.Fatalf("SearchFTS = %d results, %v; want 1", len(res), err)
	}
	if _, err := ro.Upsert([]Item{{Source: "twitter", SourceID: "2", Text: "nope"}}); err == nil {
		t.Error("Upsert on a read-only store succeeded")
	}
	ro.Close()

	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("database file changed after read-only use")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("read-only use left extra files: %v", entries)
	}
}

func TestOpenReadOnlyKeepsReadingAfterFileReplaced(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "glane.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start on lambda", URL: "https://x/1"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()

	// a sync tool renames a corrupt download over the file
	bad := filepath.Join(dir, "bad.tmp")
	os.WriteFile(bad, []byte("not a database"), 0o644)
	if err := os.Rename(bad, p); err != nil {
		t.Fatal(err)
	}

	// concurrent queries must not open new connections onto the bad file
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ro.SearchFTS("cold", Filter{}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("SearchFTS after replacement: %v", err)
	}
}

func TestOpenReadOnlyMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.db")
	if _, err := OpenReadOnly(p); err == nil {
		t.Fatal("OpenReadOnly on a missing file succeeded")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("OpenReadOnly created the missing file")
	}
}

func TestOpenReadOnlyRejectsForeignFile(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.txt")
	os.WriteFile(text, []byte("not a database"), 0o644)
	if _, err := OpenReadOnly(text); err == nil {
		t.Error("OpenReadOnly accepted a text file")
	}

	other := filepath.Join(dir, "other.db")
	db, err := sql.Open("sqlite", other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE contacts(name TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := OpenReadOnly(other); err == nil {
		t.Error("OpenReadOnly accepted a SQLite file without glane tables")
	}
}
