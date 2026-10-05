package web

import (
	"fmt"
	"net/http/httptest"
	"net/url"
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

func TestEmptyQueryListsRecentSinceDate(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	jan, jul := int64(1672531200), int64(1688169600) // 2023-01-01, 2023-07-01
	s.Upsert([]store.Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "old", URL: "http://x/1", CreatedAt: jan},
		{Source: "github", SourceID: "2", Kind: "star", Text: "fresh", URL: "http://x/2", CreatedAt: jul},
	})

	// No query, no tag: the review listing, not a blank screen.
	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "http://x/1") || !strings.Contains(body, "http://x/2") {
		t.Fatalf("empty query should list every item: %s", body)
	}

	// The date picker narrows it down.
	rec2 := httptest.NewRecorder()
	handler(s).ServeHTTP(rec2, httptest.NewRequest("GET", "/search?since=2023-05-01", nil))
	body2 := rec2.Body.String()
	if strings.Contains(body2, "http://x/1") || !strings.Contains(body2, "http://x/2") {
		t.Fatalf("since=2023-05-01 should keep only the july item: %s", body2)
	}

	// A half-typed date degrades to no filter instead of erroring out.
	rec3 := httptest.NewRecorder()
	handler(s).ServeHTTP(rec3, httptest.NewRequest("GET", "/search?since=20", nil))
	if rec3.Code != 200 || !strings.Contains(rec3.Body.String(), "http://x/1") {
		t.Fatalf("malformed date should fall back to no filter, got %d: %s", rec3.Code, rec3.Body.String())
	}
}

// The UI now sends both at once (tag links include the form, the date input
// carries the hidden tag), so the handler must narrow on the pair.
func TestTagBrowseHonorsSince(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})
	seedTagged(t, s, "2", "beta", []string{"rust"})
	// seedTagged leaves created_at at 0; date only the second one.
	s.Upsert([]store.Item{{Source: "bluesky", SourceID: "2", Kind: "like", Text: "beta", URL: "http://x/2", CreatedAt: 1688169600}})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?tag=rust&since=2023-05-01", nil))
	body := rec.Body.String()
	if strings.Contains(body, "http://x/1") || !strings.Contains(body, "http://x/2") {
		t.Fatalf("tag browse should still honor since: %s", body)
	}
}

// A review listing that stops at the cap without saying so reads as "that is
// everything that piled up".
func TestListingAnnouncesTruncation(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	items := make([]store.Item, pageLimit+1)
	for i := range items {
		items[i] = store.Item{Source: "github", SourceID: fmt.Sprint(i), Kind: "star",
			Text: "item", URL: fmt.Sprintf("http://x/%d", i), CreatedAt: int64(i + 1)}
	}
	if _, err := s.Upsert(items); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
	body := rec.Body.String()
	if !strings.Contains(body, fmt.Sprintf("%d results", pageLimit)) {
		t.Fatalf("listing must show how many it returned: %s", body)
	}
	if !strings.Contains(body, en["truncated"]) {
		t.Fatalf("a capped listing must say it is capped: %s", body)
	}

	// Under the cap, no warning — otherwise it cries wolf on every search.
	rec2 := httptest.NewRecorder()
	handler(s).ServeHTTP(rec2, httptest.NewRequest("GET", "/search?source=twitter&q=item", nil))
	if strings.Contains(rec2.Body.String(), en["truncated"]) {
		t.Fatalf("empty result set must not claim truncation: %s", rec2.Body.String())
	}
}

// The page must open on the newest items rather than a blank #results, and
// every filter control writes an operator into the query box instead of
// keeping state of its own, so the box, the rail and the CLI echo agree.
func TestIndexAutoListsAndFiltersThroughQuery(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `hx-trigger="load, submit"`) {
		t.Fatalf("index must request the newest items on load: %s", body)
	}
	for _, want := range []string{
		`data-op="tag" data-v="rust"`,       // the tag rail
		`data-op="source" data-v="bluesky"`, // a source facet…
		`data-key="2"`,                      // …reachable from the keyboard
		`data-op="since" data-v="30d"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("index filter rail missing %s: %s", want, body)
		}
	}
	// a source with nothing indexed stays in place (so 1–4 never shift) but
	// can't be picked
	if !strings.Contains(body, `data-v="github" data-key="4" aria-pressed="false" disabled`) {
		t.Fatalf("empty source facet must be disabled: %s", body)
	}
}

func TestTagRenderedAsFilterButton(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?tag=rust", nil))
	if !strings.Contains(rec.Body.String(), `data-op="tag" data-v="rust"`) {
		t.Fatalf("result tag not rendered as a filter button: %s", rec.Body.String())
	}
}

// Operators typed in the box filter like the URL parameters, and the fragment
// echoes the equivalent CLI command.
func TestSearchOperatorsInQuery(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})
	seedTagged(t, s, "2", "alpha beta", []string{"go"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=alpha+tag%3Arust+since%3A20", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "http://x/1") || strings.Contains(body, "http://x/2") {
		t.Fatalf("tag: in the query must filter like ?tag=: %s", body)
	}
	// since:20 is a date being typed: ignored, and left out of the echo
	if !strings.Contains(body, "<code>glane search alpha --tag rust</code>") {
		t.Fatalf("missing CLI echo: %s", body)
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

func TestStatsPageShowsCounts(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div class="n">1</div>`) {
		t.Fatalf("expected a stat card showing 1: %s", body)
	}
	if !strings.Contains(body, `<span class="dot bluesky"></span>bluesky</span></td>`) {
		t.Fatalf("expected bluesky row in per-source table: %s", body)
	}
}

func TestIndexHasStatsLink(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), `href="/stats"`) {
		t.Fatalf("index missing nav link to /stats: %s", rec.Body.String())
	}
}

// The rail's counts follow the search: every fragment carries fresh source and
// tag facets for htmx to swap in by id.
func TestSearchSendsFacetsOutOfBand(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})
	seedTagged(t, s, "2", "beta", []string{"go"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=alpha", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`id="rail-sources" hx-swap-oob="true"`,
		`id="rail-tags" hx-swap-oob="true"`,
		`data-v="bluesky" data-key="2" aria-pressed="false"><span class="dot bluesky"></span>bluesky<span class="n">1</span>`,
		`data-v="rust" aria-pressed="false">rust<span class="n">1</span>`,
		en["countsFTS"],
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("fragment missing %s: %s", want, body)
		}
	}
	// beta doesn't match, so its tag drops out of the rail
	if strings.Contains(body, `data-v="go"`) {
		t.Fatalf("tag of a non-matching item still in the rail: %s", body)
	}
	// twitter has no full-text match, but semantic search may still find
	// items there: a query never disables a source
	if strings.Contains(body, " disabled>") {
		t.Fatalf("source disabled under a query: %s", body)
	}

	rec = httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
	if !strings.Contains(rec.Body.String(), `data-v="twitter" data-key="1" aria-pressed="false" disabled>`) {
		t.Fatalf("empty source not disabled without a query: %s", rec.Body.String())
	}
}

// A filter the search cannot apply is dropped, never silently: the status line
// names it, so an empty or unfiltered listing is not mistaken for the answer.
func TestSearchNotesIgnoredFilters(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	for q, want := range map[string]string{
		"alpha since:abc":       fmt.Sprintf(en["noteSince"], "abc"),
		"alpha source:foo":      fmt.Sprintf(en["noteSource"], "foo"),
		"tag:go tag:rust alpha": fmt.Sprintf(en["noteRepeated"], "tag:rust"),
	} {
		rec := httptest.NewRecorder()
		handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q="+url.QueryEscape(q), nil))
		body := rec.Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("%q: missing note %q: %s", q, want, body)
		}
		if !strings.Contains(body, "http://x/1") {
			t.Errorf("%q: the ignored filter must not hide the hit: %s", q, body)
		}
	}
}

// The page reads ?q= so a search survives a reload and can be bookmarked; the
// script writes it back after each search.
func TestIndexPrefillsQueryFromURL(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/?q="+url.QueryEscape(`tag:"web dev"`), nil))
	if !strings.Contains(rec.Body.String(), `value="tag:&#34;web dev&#34;"`) {
		t.Fatalf("search box not prefilled from ?q=: %s", rec.Body.String())
	}
}
