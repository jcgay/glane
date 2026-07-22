package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

// seedTagged upserts an item, then attaches a summary + tags to it (via the
// public API, since package web can't touch the unexported store db).
func seedTagged(t *testing.T, s *store.Store, srcid, text string, tags []string) {
	t.Helper()
	if _, err := s.Upsert([]store.Item{{Source: "bluesky", SourceID: srcid, Kind: "like", Text: text, URL: "http://x/" + srcid}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.SearchFTS(text, store.Filter{})
	if err != nil || len(res) == 0 {
		t.Fatalf("seed lookup failed for %q: %v", text, err)
	}
	if err := s.SaveSummary(res[0].ID, "summary for "+srcid, tags); err != nil {
		t.Fatal(err)
	}
}

func TestSearchByTagBrowse(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})
	seedTagged(t, s, "2", "beta", []string{"go"})

	req := httptest.NewRequest("GET", "/search?tag=rust", nil)
	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(body, "http://x/1") {
		t.Fatalf("rust item missing from tag browse: %s", body)
	}
	if strings.Contains(body, "http://x/2") {
		t.Fatalf("go item should not appear in tag=rust browse: %s", body)
	}
}

func TestSearchTextAndTagFilter(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})
	seedTagged(t, s, "2", "beta", []string{"go"})

	// text matches item 1, tag rust matches item 1 → present
	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=alpha&tag=rust", nil))
	if !strings.Contains(rec.Body.String(), "http://x/1") {
		t.Fatalf("text+tag should return item 1: %s", rec.Body.String())
	}
	// text matches item 1 but tag go does not → excluded (no results)
	rec2 := httptest.NewRecorder()
	handler(s).ServeHTTP(rec2, httptest.NewRequest("GET", "/search?q=alpha&tag=go", nil))
	if strings.Contains(rec2.Body.String(), "http://x/1") {
		t.Fatalf("tag=go must exclude the rust item even when text matches: %s", rec2.Body.String())
	}
}

func TestTagRenderedAsClickableLink(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?tag=rust", nil))
	if !strings.Contains(rec.Body.String(), `hx-get="/search?tag=rust"`) {
		t.Fatalf("tag not rendered as an htmx link: %s", rec.Body.String())
	}
}

func TestIndexListsTagsForBrowsing(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(body, "rust") {
		t.Fatalf("tag not listed on index for browsing: %s", body)
	}
	if !strings.Contains(body, `hx-get="/search?tag=rust"`) {
		t.Fatalf("index tag not rendered as an htmx link: %s", body)
	}
}

func TestEnrichedLinkShownAsPrimary(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.Upsert([]store.Item{{Source: "bluesky", SourceID: "1", Kind: "like", Text: "check this", URL: "http://bsky/post/1"}})
	res, _ := s.SearchFTS("check", store.Filter{})
	if err := s.SaveEnrichment(res[0].ID, store.Enrichment{LinkURL: "http://realsite.com/article", Status: "ok"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=check", nil))
	body := rec.Body.String()
	// enriched link is the primary destination…
	if !strings.Contains(body, `href="http://realsite.com/article"`) {
		t.Fatalf("enriched link not shown as primary: %s", body)
	}
	// …and the source post stays reachable as a secondary link
	if !strings.Contains(body, `href="http://bsky/post/1"`) {
		t.Fatalf("source post link missing: %s", body)
	}
}

func TestSearchRendersHighlightedExcerpt(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "cool physics", URL: "http://x"}})
	res, _ := s.SearchFTS("physics", store.Filter{})
	// Body match, with hostile HTML mixed in, so we can check escaping too.
	s.SaveEnrichment(res[0].ID, store.Enrichment{
		Text:   `<script>alert(1)</script> a note on quantum entanglement today`,
		Status: "ok",
	})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=entanglement", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "<mark>entanglement</mark>") {
		t.Fatalf("matched term not highlighted in excerpt: %s", body)
	}
	// The article body's own HTML must be escaped, not rendered.
	if strings.Contains(body, "<script>alert") {
		t.Fatalf("article HTML leaked unescaped (XSS): %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped article HTML in excerpt: %s", body)
	}
}

func TestSearchHighlightsShownSummary(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x", URL: "http://x"}})
	res, _ := s.SearchFTS("x", store.Filter{})
	s.SaveSummary(res[0].ID, "a thread about lambda cold starts", nil)

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=lambda", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "<mark>lambda</mark>") {
		t.Fatalf("term in shown summary not highlighted: %s", body)
	}
}

func TestSearchFragmentRendersHits(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "aws lambda cold start", URL: "http://x"}})

	req := httptest.NewRequest("GET", "/search?q=lambda", nil)
	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "lambda") {
		t.Fatalf("fragment missing hit: %s", rec.Body.String())
	}
}
