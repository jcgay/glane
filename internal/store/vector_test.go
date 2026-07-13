package store

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadEmbeddings(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x"}})
	all, _ := s.AllEmbeddings("m", Filter{})
	if len(all) != 0 {
		t.Fatal("expected none yet")
	}
	if err := s.SaveEmbedding(1, "m", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	all, _ = s.AllEmbeddings("m", Filter{})
	if len(all) != 1 || all[0].Vec[2] != 3 {
		t.Fatalf("bad load: %+v", all)
	}
}

func TestAllEmbeddingsFilterBySource(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "github", SourceID: "1", Kind: "repo", Text: "x"},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "y"},
	})
	if err := s.SaveEmbedding(1, "m", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEmbedding(2, "m", []float32{4, 5, 6}); err != nil {
		t.Fatal(err)
	}

	all, err := s.AllEmbeddings("m", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	gh, err := s.AllEmbeddings("m", Filter{Source: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gh) != 1 || gh[0].ID != 1 {
		t.Fatalf("expected only github item (id 1), got %+v", gh)
	}
}
