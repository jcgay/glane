package store

import (
	"path/filepath"
	"testing"
)

func TestStatsZeroOnEmptyDB(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 0 || len(st.BySource) != 0 || st.Enriched != 0 || st.Summarized != 0 ||
		st.Embedded != 0 || st.DistinctTags != 0 || len(st.LastSyncBySource) != 0 {
		t.Fatalf("expected all-zero stats on empty db, got %+v", st)
	}
}

func TestStatsAggregatesAcrossSources(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// twitter: 2 items — one fully enriched+summarized+tagged+embedded, one plain.
	seedEnriched(t, s, 1, "twitter", "kube post")
	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "9", Kind: "like", Text: "plain", URL: "http://x/9"}}); err != nil {
		t.Fatal(err)
	}
	// github: 1 item, enriched but never summarized.
	seedEnriched(t, s, 2, "github", "repo post")

	var id1 int64
	if err := s.db.QueryRow(`SELECT id FROM items WHERE source=? AND source_id=?`, "twitter", itoa(1)).Scan(&id1); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSummary(id1, "a summary", []string{"kubernetes", "devops"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEmbedding(id1, "m", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor("github", "cursor-1"); err != nil {
		t.Fatal(err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 {
		t.Fatalf("Total = %d, want 3", st.Total)
	}
	want := map[string]int{"twitter": 2, "github": 1}
	if len(st.BySource) != 2 {
		t.Fatalf("BySource = %+v, want 2 entries", st.BySource)
	}
	for _, sc := range st.BySource {
		if want[sc.Source] != sc.Count {
			t.Fatalf("BySource[%s] = %d, want %d", sc.Source, sc.Count, want[sc.Source])
		}
	}
	if st.Enriched != 2 {
		t.Fatalf("Enriched = %d, want 2", st.Enriched)
	}
	if st.Summarized != 1 {
		t.Fatalf("Summarized = %d, want 1", st.Summarized)
	}
	if st.Embedded != 1 {
		t.Fatalf("Embedded = %d, want 1", st.Embedded)
	}
	if st.DistinctTags != 2 {
		t.Fatalf("DistinctTags = %d, want 2", st.DistinctTags)
	}
	if len(st.LastSyncBySource) != 1 || st.LastSyncBySource[0].Source != "github" {
		t.Fatalf("LastSyncBySource = %+v, want one entry for github", st.LastSyncBySource)
	}
	if st.LastSyncBySource[0].UpdatedAt == 0 {
		t.Fatal("expected non-zero UpdatedAt for github sync")
	}
}
