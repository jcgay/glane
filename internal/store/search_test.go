package store

import (
	"path/filepath"
	"strings"
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

func TestSearchFTSSnippetAndExcerpt(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "github", SourceID: "1", Kind: "star", Text: "cool physics"},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "x"},
	})
	// Match lives in the hidden article body, not in Text or a summary.
	s.SaveEnrichment(1, Enrichment{Text: "a long read about quantum entanglement breakthroughs in 2026"})
	// Match lives in the summary, which the card already shows.
	s.SaveSummary(2, "a thread on lambda cold starts", nil)

	// Body match: snippet is populated, wraps the term, and Excerpt surfaces it
	// because it is not already in the displayed text.
	res, err := s.SearchFTS("entanglement", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("want 1 hit, got %d", len(res))
	}
	if !strings.Contains(res[0].Snippet, MarkStart+"entanglement"+MarkEnd) {
		t.Fatalf("snippet should wrap the matched term, got %q", res[0].Snippet)
	}
	if res[0].Excerpt() == "" {
		t.Fatal("Excerpt should surface a body match not shown in the summary/text")
	}

	// Summary match: Excerpt is suppressed to avoid echoing what the card shows.
	res, _ = s.SearchFTS("lambda", Filter{})
	if len(res) != 1 {
		t.Fatalf("want 1 hit, got %d", len(res))
	}
	if res[0].Excerpt() != "" {
		t.Fatalf("Excerpt should be empty when the match is already in the summary, got %q", res[0].Excerpt())
	}
}

func TestSearchFTSHighlightsDisplayedFields(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "github", SourceID: "1", Kind: "star", Text: "cool physics"},             // match will be in the title
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "raw useState in a tweet"}, // match in the raw text (no summary)
	})
	s.SaveEnrichment(1, Enrichment{Title: "Kubernetes networking guide", Text: "unrelated body"})
	s.SaveSummary(1, "a summary mentioning kubernetes clearly", nil)

	// Title match: the shown title is highlighted, and the excerpt is suppressed
	// (the term is already visible on the card).
	res, err := s.SearchFTS("kubernetes", Filter{Source: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res[0].TitleMarked(), MarkStart+"Kubernetes"+MarkEnd) {
		t.Fatalf("title should be highlighted, got %q", res[0].TitleMarked())
	}
	if !strings.Contains(res[0].BodyMarked(), MarkStart+"kubernetes"+MarkEnd) {
		t.Fatalf("summary should be highlighted, got %q", res[0].BodyMarked())
	}
	if res[0].Excerpt() != "" {
		t.Fatalf("excerpt should be suppressed when the match is in shown fields, got %q", res[0].Excerpt())
	}

	// Raw-text match (no summary): the body falls back to Text, highlighted.
	res, _ = s.SearchFTS("useState", Filter{Source: "twitter"})
	if !strings.Contains(res[0].BodyMarked(), MarkStart+"useState"+MarkEnd) {
		t.Fatalf("raw text should be highlighted, got %q", res[0].BodyMarked())
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

func TestParseSince(t *testing.T) {
	if ts, err := ParseSince(""); ts != 0 || err != nil {
		t.Errorf("ParseSince(\"\") = (%d, %v), want (0, nil)", ts, err)
	}
	if ts, err := ParseSince("2023"); ts <= 0 || err != nil {
		t.Errorf("ParseSince(\"2023\") = (%d, %v), want (positive, nil)", ts, err)
	}
	if ts, err := ParseSince("2023-06-15"); ts <= 0 || err != nil {
		t.Errorf("ParseSince(\"2023-06-15\") = (%d, %v), want (positive, nil)", ts, err)
	}
	if _, err := ParseSince("nope"); err == nil {
		t.Errorf("ParseSince(\"nope\") = nil error, want an error")
	}
}

func TestRecentNewestFirstAndFilters(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	jan, jun, jul := int64(1672531200), int64(1685577600), int64(1688169600) // 2023-01-01, 2023-06-01, 2023-07-01
	s.Upsert([]Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "old post", CreatedAt: jan},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "summer post", CreatedAt: jun},
		{Source: "github", SourceID: "3", Kind: "star", Text: "a repo", CreatedAt: jul},
	})

	res, err := s.Recent(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("want all 3 items, got %d", len(res))
	}
	if res[0].SourceID != "3" || res[2].SourceID != "1" {
		t.Fatalf("want newest first, got %s…%s", res[0].SourceID, res[2].SourceID)
	}

	since, _ := ParseSince("2023-05-01")
	res, _ = s.Recent(Filter{Since: since})
	if len(res) != 2 {
		t.Fatalf("since 2023-05-01: want 2 items, got %d", len(res))
	}
	res, _ = s.Recent(Filter{Since: since, Source: "github"})
	if len(res) != 1 || res[0].Source != "github" {
		t.Fatalf("since + source filter failed: %+v", res)
	}
	if res, _ = s.Recent(Filter{Limit: 1}); len(res) != 1 {
		t.Fatalf("limit ignored: got %d items", len(res))
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
