package store

import (
	"path/filepath"
	"testing"
)

func TestPendingThenSaveEnrichment(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x", URL: "http://a"}})

	p, err := s.PendingEnrichment(10)
	if err != nil || len(p) != 1 {
		t.Fatalf("pending=%d err=%v", len(p), err)
	}
	if err := s.SaveEnrichment(p[0].ID, e("Title", "body about lambda", "ok")); err != nil {
		t.Fatal(err)
	}
	// Now it is no longer pending...
	p2, _ := s.PendingEnrichment(10)
	if len(p2) != 0 {
		t.Fatalf("still pending: %d", len(p2))
	}
	// ...and its article text is searchable via FTS.
	res, _ := s.SearchFTS("lambda", Filter{})
	if len(res) != 1 {
		t.Fatalf("article text not indexed, hits=%d", len(res))
	}
}

// TestResetEnrichment proves --force's backing call makes an already-fetched
// item pending again and clears its stale extracted content.
func TestResetEnrichment(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x", URL: "http://a"}})
	p, _ := s.PendingEnrichment(10)
	s.SaveEnrichment(p[0].ID, e("Title", "body", "ok"))

	n, err := s.ResetEnrichment()
	if err != nil || n != 1 {
		t.Fatalf("reset n=%d err=%v", n, err)
	}
	p2, _ := s.PendingEnrichment(10)
	if len(p2) != 1 {
		t.Fatalf("item not pending after reset: %d", len(p2))
	}
	var title, text string
	s.db.QueryRow(`SELECT article_title, article_text FROM items WHERE id=?`, p[0].ID).Scan(&title, &text)
	if title != "" || text != "" {
		t.Fatalf("stale content not cleared: %q / %q", title, text)
	}
}

// e is a tiny helper mirroring enrich.Enrichment fields the store needs.
func e(title, text, status string) Enrichment {
	return Enrichment{Title: title, Text: text, Status: status}
}

// TestReupsertPreservesEnrichment proves that re-importing an item (e.g. a
// re-run of `glane import`) never clobbers enrichment columns written by a
// prior `enrich` run — Upsert's ON CONFLICT only touches base columns.
func TestReupsertPreservesEnrichment(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()

	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x", URL: "http://a"}}); err != nil {
		t.Fatal(err)
	}
	p, err := s.PendingEnrichment(10)
	if err != nil || len(p) != 1 {
		t.Fatalf("pending=%d err=%v", len(p), err)
	}
	if err := s.SaveEnrichment(p[0].ID, e("My Title", "article body text", "ok")); err != nil {
		t.Fatal(err)
	}

	// Re-import the same item: same source+source_id, base columns only.
	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x", URL: "http://a"}}); err != nil {
		t.Fatal(err)
	}

	var articleTitle, articleText, fetchStatus string
	if err := s.db.QueryRow(`SELECT article_title, article_text, fetch_status FROM items WHERE id=?`, p[0].ID).
		Scan(&articleTitle, &articleText, &fetchStatus); err != nil {
		t.Fatal(err)
	}
	if articleTitle != "My Title" {
		t.Fatalf("article_title clobbered: %q", articleTitle)
	}
	if articleText != "article body text" {
		t.Fatalf("article_text clobbered: %q", articleText)
	}
	if fetchStatus != "ok" {
		t.Fatalf("fetch_status clobbered: %q", fetchStatus)
	}
}
