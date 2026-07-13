package search

import (
	"path/filepath"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestHybridFallsBackToFTS(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Upsert([]store.Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start of aws lambda"},
		{Source: "github", SourceID: "2", Kind: "star", Text: "lambda calculus notes"},
	})

	want, err := s.SearchFTS("lambda", store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Hybrid(s, nil, "lambda", store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("Hybrid with nil client: want %d results, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Fatalf("Hybrid diverged from SearchFTS at %d: want id %d, got %d", i, want[i].ID, got[i].ID)
		}
	}
}
