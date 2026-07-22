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

func TestSearchFTSPrefixOnLastToken(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "github", SourceID: "1", Kind: "star", Text: "prettier useTabs option"},
		{Source: "github", SourceID: "2", Kind: "star", Text: "async rust runtime"},
	})

	// Last token is a prefix: "useTa" matches "useTabs".
	res, err := s.SearchFTS("useTa", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("want 1 prefix hit for useTa, got %d", len(res))
	}

	// Earlier tokens stay exact, only the last is a prefix.
	res, _ = s.SearchFTS("async ru", Filter{})
	if len(res) != 1 {
		t.Fatalf("want 1 hit for 'async ru', got %d", len(res))
	}
}

func TestSearchFTSEmptyQuery(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start of aws lambda"},
	})

	res, err := s.SearchFTS("", Filter{})
	if err != nil || res != nil {
		t.Fatalf("empty query: want (nil, nil), got (%v, %v)", res, err)
	}

	res, err = s.SearchFTS("   ", Filter{})
	if err != nil || res != nil {
		t.Fatalf("whitespace query: want (nil, nil), got (%v, %v)", res, err)
	}
}

func TestSearchFTSHandlesSpecialChars(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "learning c++ templates"},
	})

	res, err := s.SearchFTS("c++", Filter{})
	if err != nil {
		t.Fatalf("c++ query should not error: %v", err)
	}
	if len(res) < 1 {
		t.Fatalf("want at least 1 hit for c++, got %d", len(res))
	}

	_, err = s.SearchFTS(`a "quote`, Filter{})
	if err != nil {
		t.Fatalf("unbalanced quote query should not error: %v", err)
	}
}
