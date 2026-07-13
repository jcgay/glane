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
