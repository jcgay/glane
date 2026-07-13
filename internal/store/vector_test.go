package store

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadEmbeddings(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x"}})
	all, _ := s.AllEmbeddings("m")
	if len(all) != 0 {
		t.Fatal("expected none yet")
	}
	if err := s.SaveEmbedding(1, "m", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	all, _ = s.AllEmbeddings("m")
	if len(all) != 1 || all[0].Vec[2] != 3 {
		t.Fatalf("bad load: %+v", all)
	}
}
