package store

import (
	"path/filepath"
	"testing"
)

func seedEnriched(t *testing.T, s *Store, id int, src, txt string) {
	t.Helper()
	if _, err := s.Upsert([]Item{{Source: src, SourceID: itoa(id), Kind: "like", Text: txt, URL: "http://x/" + itoa(id)}}); err != nil {
		t.Fatal(err)
	}
	// mark it enriched so PendingSummary picks it up
	var rowid int64
	s.db.QueryRow(`SELECT id FROM items WHERE source=? AND source_id=?`, src, itoa(id)).Scan(&rowid)
	if _, err := s.db.Exec(`UPDATE items SET article_text=?, fetch_status='ok' WHERE id=?`, "article "+txt, rowid); err != nil {
		t.Fatal(err)
	}
}

func itoa(i int) string { return string(rune('0' + i)) }

func TestSummaryAndTagsRoundTrip(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	seedEnriched(t, s, 1, "twitter", "kube post")

	pend, err := s.PendingSummary(10)
	if err != nil || len(pend) != 1 {
		t.Fatalf("pending=%d err=%v", len(pend), err)
	}
	if err := s.SaveSummary(pend[0].ID, "a summary about kubernetes", []string{"kubernetes", "devops"}); err != nil {
		t.Fatal(err)
	}
	// no longer pending
	if p, _ := s.PendingSummary(10); len(p) != 0 {
		t.Fatalf("still pending: %d", len(p))
	}
	// summary searchable via FTS
	if res, _ := s.SearchFTS("kubernetes", Filter{}); len(res) != 1 {
		t.Fatalf("summary not FTS-indexed")
	}
	// tag counts
	tc, _ := s.TagCounts()
	if len(tc) != 2 {
		t.Fatalf("tag counts %+v", tc)
	}
	// TagsFor
	m, _ := s.TagsFor([]int64{pend[0].ID})
	if len(m[pend[0].ID]) != 2 {
		t.Fatalf("tagsfor %+v", m)
	}
}

func TestTagFilterConstrainsFTS(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	seedEnriched(t, s, 1, "twitter", "alpha")
	seedEnriched(t, s, 2, "github", "alpha")
	// tag only item 1
	var id1 int64
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(1)).Scan(&id1)
	s.SaveSummary(id1, "s1", []string{"rust"})
	var id2 int64
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(2)).Scan(&id2)
	s.SaveSummary(id2, "s2", []string{"go"})

	res, err := s.SearchFTS("alpha", Filter{Tag: "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != id1 {
		t.Fatalf("tag filter failed: %+v", res)
	}

	// tag browse (no query)
	bt, _ := s.Recent(Filter{Tag: "go"})
	if len(bt) != 1 || bt[0].ID != id2 {
		t.Fatalf("tag browse failed: %+v", bt)
	}
}

func TestTagFilterConstrainsEmbeddings(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	seedEnriched(t, s, 1, "twitter", "alpha")
	seedEnriched(t, s, 2, "github", "beta")
	var id1, id2 int64
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(1)).Scan(&id1)
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(2)).Scan(&id2)
	s.SaveSummary(id1, "s1", []string{"rust"})
	s.SaveSummary(id2, "s2", []string{"go"})
	if err := s.SaveEmbedding(id1, "m", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEmbedding(id2, "m", []float32{0, 1}); err != nil {
		t.Fatal(err)
	}

	if all, _ := s.AllEmbeddings("m", Filter{}); len(all) != 2 {
		t.Fatalf("want 2 embeddings unfiltered, got %d", len(all))
	}
	tagged, err := s.AllEmbeddings("m", Filter{Tag: "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tagged) != 1 || tagged[0].ID != id1 {
		t.Fatalf("tag filter on embeddings failed: %+v", tagged)
	}
}
