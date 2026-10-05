package store

import (
	"path/filepath"
	"testing"
)

func TestFacets(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Upsert([]Item{
		{Source: "twitter", SourceID: "1", Text: "sqlite tips", CreatedAt: 300},
		{Source: "twitter", SourceID: "2", Text: "kubernetes", CreatedAt: 300},
		{Source: "github", SourceID: "3", Text: "go sqlite driver", CreatedAt: 300},
		{Source: "github", SourceID: "4", Text: "old sqlite fork", CreatedAt: 100},
	}); err != nil {
		t.Fatal(err)
	}
	tag := func(srcid string, tags ...string) {
		res, _ := s.SearchFTS(map[string]string{"1": "tips", "3": "driver", "4": "fork"}[srcid], Filter{})
		if err := s.SaveSummary(res[0].ID, "", tags); err != nil {
			t.Fatal(err)
		}
	}
	tag("1", "db")
	tag("3", "db", "go")
	tag("4", "go")

	count := func(fc Facets) (map[string]int, map[string]int) {
		src, tags := map[string]int{}, map[string]int{}
		for _, sc := range fc.BySource {
			src[sc.Source] = sc.Count
		}
		for _, tc := range fc.Tags {
			tags[tc.Tag] = tc.Count
		}
		return src, tags
	}

	// a query counts only its full-text matches
	fc, err := s.Facets("sqlite", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	src, tags := count(fc)
	if fc.Total != 3 || src["twitter"] != 1 || src["github"] != 2 || tags["db"] != 2 || tags["go"] != 2 {
		t.Fatalf("Facets(sqlite) = %+v", fc)
	}

	// each dimension ignores its own filter and honours the others
	if fc, err = s.Facets("sqlite", Filter{Source: "github", Tag: "go", Since: 200}); err != nil {
		t.Fatal(err)
	}
	src, tags = count(fc)
	if src["github"] != 1 || src["twitter"] != 0 || fc.Total != 1 {
		t.Fatalf("source counts must ignore source:, keep tag: and since:, got %+v", fc)
	}
	if tags["db"] != 1 || tags["go"] != 1 {
		t.Fatalf("tag counts must ignore tag:, keep source: and since:, got %+v", fc)
	}

	// no query: the filtered listing
	if fc, err = s.Facets("", Filter{Since: 200}); err != nil {
		t.Fatal(err)
	}
	if src, _ := count(fc); fc.Total != 3 || src["twitter"] != 2 {
		t.Fatalf("Facets(\"\", since) = %+v", fc)
	}
}
