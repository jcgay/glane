package store

import (
	"path/filepath"
	"testing"
)

func TestSearchFTSMatchesAndFilters(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start of aws lambda"},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "kubernetes networking"},
		{Source: "github", SourceID: "3", Kind: "star", Text: "lambda calculus notes"},
	})

	res, err := s.SearchFTS("lambda", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 lambda hits, got %d", len(res))
	}

	res, _ = s.SearchFTS("lambda", Filter{Source: "github"})
	if len(res) != 1 || res[0].Source != "github" {
		t.Fatalf("source filter failed: %+v", res)
	}
}
