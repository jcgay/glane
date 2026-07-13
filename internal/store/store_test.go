package store

import (
	"path/filepath"
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
