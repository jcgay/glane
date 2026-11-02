package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	Handler(s).ServeHTTP(rec, req)
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=alpha&tag=rust", nil))
	if !strings.Contains(rec.Body.String(), "http://x/1") {
		t.Fatalf("text+tag should return item 1: %s", rec.Body.String())
	}
	// text matches item 1 but tag go does not → excluded (no results)
	rec2 := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec2, httptest.NewRequest("GET", "/search?q=alpha&tag=go", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "http://x/1") || !strings.Contains(body, "http://x/2") {
		t.Fatalf("empty query should list every item: %s", body)
	}

	// The date picker narrows it down.
	rec2 := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec2, httptest.NewRequest("GET", "/search?since=2023-05-01", nil))
	body2 := rec2.Body.String()
	if strings.Contains(body2, "http://x/1") || !strings.Contains(body2, "http://x/2") {
		t.Fatalf("since=2023-05-01 should keep only the july item: %s", body2)
	}

	// A half-typed date degrades to no filter instead of erroring out.
	rec3 := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec3, httptest.NewRequest("GET", "/search?since=20", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?tag=rust&since=2023-05-01", nil))
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

	t.Setenv("GLANE_EMBED_URL", "") // the full-text count is exact
	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
	body := rec.Body.String()
	if !strings.Contains(body, fmt.Sprintf("%d of %d results", pageLimit, pageLimit+1)) {
		t.Fatalf("listing must show how many it returned out of how many: %s", body)
	}
	if !strings.Contains(body, "1 more") || !strings.Contains(body, `hx-vals='{"n": 100}'`) {
		t.Fatalf("a full page must offer the next one, sized to what is left: %s", body)
	}

	// the next page is the same search, one page longer, and the last one
	// offers nothing more
	rec = httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?n=100", nil))
	body = rec.Body.String()
	if !strings.Contains(body, fmt.Sprintf("%d results", pageLimit+1)) || strings.Contains(body, `id="more"`) {
		t.Fatalf("?n=100 must list all %d items with no more button: %s", pageLimit+1, body)
	}

	// Under the cap, no warning — otherwise it cries wolf on every search.
	rec2 := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec2, httptest.NewRequest("GET", "/search?source=twitter&q=item", nil))
	if strings.Contains(rec2.Body.String(), en["hasMore"]) {
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?tag=rust", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=alpha+tag%3Arust+since%3A20", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=check", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=entanglement", nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=lambda", nil))
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
	Handler(s).ServeHTTP(rec, req)

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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div class="n">1</div>`) {
		t.Fatalf("expected a stat card showing 1: %s", body)
	}
	// every row leads to its listing
	for _, want := range []string{
		`<a class="src" href="/?q=source%3abluesky"><span class="dot bluesky"></span>bluesky</a>`,
		`<a href="/?q=tag%3arust">rust</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stats page missing %s: %s", want, body)
		}
	}
}

func TestIndexHasStatsLink(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), `href="/stats"`) {
		t.Fatalf("index missing nav link to /stats: %s", rec.Body.String())
	}
}

// The PWA install needs a reachable manifest whose icons are served too.
func TestManifestAndIconsServed(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), `rel="manifest" href="/static/manifest.json"`) {
		t.Fatalf("index missing manifest link")
	}
	var m struct {
		StartURL string `json:"start_url"`
		Icons    []struct{ Src string }
	}
	rec = httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/static/manifest.json", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || m.StartURL != "/" || len(m.Icons) == 0 {
		t.Fatalf("bad manifest (%v): %s", err, rec.Body.String())
	}
	for _, ic := range m.Icons {
		rec = httptest.NewRecorder()
		Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", ic.Src, nil))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Errorf("icon %s: %d %s", ic.Src, rec.Code, rec.Header().Get("Content-Type"))
		}
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q=alpha", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`id="rail-sources" hx-swap-oob="true"`,
		`id="rail-tags" hx-swap-oob="true"`,
		`data-v="bluesky" data-key="2" aria-pressed="false"><span class="dot bluesky"></span>bluesky<span class="n">1</span>`,
		`data-v="rust" aria-pressed="false"><span class="t">rust</span><span class="n">1</span>`,
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
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
		Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q="+url.QueryEscape(q), nil))
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
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/?q="+url.QueryEscape(`tag:"web dev"`), nil))
	if !strings.Contains(rec.Body.String(), `value="tag:&#34;web dev&#34;"`) {
		t.Fatalf("search box not prefilled from ?q=: %s", rec.Body.String())
	}
}

func TestAppIconIsEmbeddedPNG(t *testing.T) {
	icon := AppIcon()
	if !bytes.HasPrefix(icon, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("AppIcon is not a PNG (%d bytes)", len(icon))
	}
}

// A year button writes since:Y until:Y: the listing keeps that year only, and
// the rail marks it pressed while still counting every year.
func TestSearchBrowsesOneYear(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	at := func(y int) int64 { return time.Date(y, 6, 1, 12, 0, 0, 0, time.Local).Unix() }
	if _, err := s.Upsert([]store.Item{
		{Source: "github", SourceID: "1", Kind: "star", Text: "from twenty-three", URL: "http://x/1", CreatedAt: at(2023)},
		{Source: "github", SourceID: "2", Kind: "star", Text: "from twenty-four", URL: "http://x/2", CreatedAt: at(2024)},
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?q="+url.QueryEscape("since:2023 until:2023"), nil))
	body := rec.Body.String()
	if !strings.Contains(body, "twenty-three") || strings.Contains(body, "twenty-four") {
		t.Fatalf("since:2023 until:2023 must keep 2023 only: %s", body)
	}
	if !strings.Contains(body, `data-v="2023" aria-pressed="true"`) || !strings.Contains(body, `data-v="2024" aria-pressed="false">2024<span class="n">1</span>`) {
		t.Fatalf("the rail must press 2023 and still count 2024: %s", body)
	}
	if !strings.Contains(body, "glane search --since 2023 --until 2023") {
		t.Fatalf("the CLI echo must carry --until: %s", body)
	}
}

// The rail leads with the most distinctive tags and folds the rest: a tag on
// most of the listing ranks last, the browsed tag always shows, and it leads
// each result's tags.
func TestRailFoldsTagsAndDemotesBroadOnes(t *testing.T) {
	var tags []store.TagCount
	tags = append(tags, store.TagCount{Tag: "everything", Count: 90})
	for i := range topTags + 5 {
		tags = append(tags, store.TagCount{Tag: fmt.Sprintf("t%02d", i), Count: 30 - i})
	}
	r := newRail(store.Facets{Total: 3, TagBase: 100, Tags: tags}, query{Tag: "t24"})
	if len(r.Tags) != topTags+1 || r.Tags[0].Tag != "t24" || r.Tags[1].Tag != "t00" {
		t.Fatalf("top tags = %+v, want the browsed t24, then t00…", r.Tags)
	}
	if last := r.MoreTags[len(r.MoreTags)-1]; last.Tag != "everything" {
		t.Fatalf("a tag on 90%% of items must rank last, got %+v", r.MoreTags)
	}

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"a", "b", "c", "rust"})
	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search?tag=rust", nil))
	if !strings.Contains(rec.Body.String(), `<span class="tags">
        <button type="button" class="tag" data-op="tag" data-v="rust"`) {
		t.Fatalf("the browsed tag must lead the result's tags: %s", rec.Body.String())
	}
}

// A source is behind only when none of its cursors moved for a month: a
// quiet feed (mastodon:favourites) next to a busy one is not a stale source.
func TestSyncLineFlagsOnlyStaleSources(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	ago := func(days int) int64 { return now.AddDate(0, 0, -days).Unix() }
	l := newSyncLine([]store.SourceSync{
		{Source: "github", UpdatedAt: ago(1)},
		{Source: "mastodon:favourites", UpdatedAt: ago(100)},
		{Source: "mastodon:bookmarks", UpdatedAt: ago(12)},
		{Source: "bluesky:likes", UpdatedAt: ago(40)},
		{Source: "bluesky:authorfeed", UpdatedAt: ago(45)},
	}, now)
	if l.Latest != ago(1) || len(l.Stale) != 1 || l.Stale[0] != (store.SourceSync{Source: "bluesky", UpdatedAt: ago(40)}) {
		t.Fatalf("got %+v, want latest 1 day ago and only bluesky stale since 40 days", l)
	}
}

// Counts read the way each language writes them: English pluralises zero,
// French keeps it singular and groups thousands with a narrow space.
func TestCountsFollowTheLanguage(t *testing.T) {
	f := funcs["num"].(func(int, catalog) string)
	r := funcs["results"].(func(int, catalog) string)
	for _, tc := range []struct{ got, want string }{
		{f(8306, en), "8,306"}, {f(8306, fr), "8 306"}, {f(1234567, en), "1,234,567"}, {f(42, fr), "42"}, {f(0, en), "0"},
		{r(0, en), "results"}, {r(1, en), "result"}, {r(0, fr), "résultat"}, {r(2, fr), "résultats"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

// A listing of exactly one page has nothing more to offer, even though it
// filled the page.
func TestFullPageWithNothingMoreOffersNoMore(t *testing.T) {
	t.Setenv("GLANE_EMBED_URL", "")
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	items := make([]store.Item, pageLimit)
	for i := range items {
		items[i] = store.Item{Source: "github", SourceID: fmt.Sprint(i), Kind: "star",
			Text: "item", URL: fmt.Sprintf("http://x/%d", i), CreatedAt: int64(i + 1)}
	}
	if _, err := s.Upsert(items); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	Handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/search", nil))
	if body := rec.Body.String(); strings.Contains(body, `id="more"`) || strings.Contains(body, en["hasMore"]) {
		t.Fatalf("all %d items are listed, nothing more to offer: %s", pageLimit, body)
	}
}
