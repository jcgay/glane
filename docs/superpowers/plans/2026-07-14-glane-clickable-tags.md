# Clickable Tags (Web UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make result tags clickable in the web UI — clicking a tag browses everything with it — by wiring the already-existing server-side tag support into the `/search` handler and `results.html`.

**Architecture:** The `/search` handler reads a `tag` param and routes like the CLI (`store.ByTag` for tag-only browse, `Filter.Tag` through `search.Hybrid` when there's a query). `results.html` renders each tag as an htmx link. No new search logic.

**Tech Stack:** Go stdlib + `html/template`; htmx already vendored. No new dependencies.

## Global Constraints

- Module path `github.com/jcgay/glane` (verbatim).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies. No change to search logic — only the web handler routing + template.
- The tag value in the htmx link MUST be URL-encoded via `{{. | urlquery}}` (`hx-get` is not a template-recognized URL attribute, so query escaping is not automatic).
- Web UI stays local-only; existing behavior (text search, source select) unchanged.
- Tests httptest-only (drive `handler(s)`), no real network.
- Commit: English, leading literal Unicode gitmoji, body explains *why*.

---

### Task 1: Tag param in the handler + clickable tags in the template

**Files:**
- Modify: `internal/web/web.go` (`/search` reads `tag`, routes ByTag vs Hybrid+Filter.Tag)
- Modify: `internal/web/templates/results.html` (tags → htmx links)
- Modify: `internal/web/templates/index.html` (clickable `.tag` style)
- Test: `internal/web/web_test.go`

**Interfaces:**
- Consumes: `store.ByTag(tag, Filter) ([]Result, error)`, `search.Hybrid`, `store.Filter{Source,Tag,Limit}`, `store.Result`, `s.AttachTags`, `s.SearchFTS`, `s.SaveSummary`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/web/web_test.go` (package `web`; needs `strings`):
```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/web/ -run 'Tag'`
Expected: FAIL — handler ignores `tag` (byTag browse returns empty; clickable link not rendered).

- [ ] **Step 3: Update the `/search` handler**

In `internal/web/web.go`, replace the `/search` handler body with:
```go
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		tag := r.URL.Query().Get("tag")
		if q == "" && tag == "" {
			w.Write([]byte(""))
			return
		}
		var res []store.Result
		var err error
		if q == "" {
			res, err = s.ByTag(tag, store.Filter{Limit: 50})
		} else {
			res, err = search.Hybrid(s, gembed.FromEnv(), q, store.Filter{
				Source: r.URL.Query().Get("source"),
				Tag:    tag,
				Limit:  50,
			})
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := s.AttachTags(res); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "results.html", res); err != nil {
			log.Printf("glane: render results.html: %v", err)
		}
	})
```

- [ ] **Step 4: Make tags clickable in `results.html`**

Replace the tags line in `internal/web/templates/results.html`:
```html
  {{if .Tags}}<div class="tags">{{range .Tags}}<a class="tag" href="#" hx-get="/search?tag={{. | urlquery}}" hx-target="#results">{{.}}</a> {{end}}</div>{{end}}
```
(Keep the trailing space after `</a>` for spacing between tags.)

- [ ] **Step 5: Style `.tag` as clickable in `index.html`**

In `internal/web/templates/index.html`'s `<style>`, replace the `.tag` rule and add a hover:
```css
    .tag { font-size: .7rem; background: #eef; border-radius: .3rem; padding: .05rem .35rem; cursor: pointer; text-decoration: none; color: #334; }
    .tag:hover { background: #dde; }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/web/ -v`
Expected: PASS — the three new tag tests plus the existing `TestSearchFragmentRendersHits` (which uses `?q=lambda`, unchanged).

- [ ] **Step 7: Manual smoke check (optional but quick)**

Run:
```bash
mise exec -- go build -o /tmp/glane . && GLANE_DB=/tmp/glane-tags.db /tmp/glane serve --port 8091 &
sleep 1
curl -s "http://127.0.0.1:8091/search?tag=anything" | head; kill %1
```
Expected: a 200 response (likely `Aucun résultat.` on an empty DB) — confirms the tag route is wired and doesn't error.

- [ ] **Step 8: Full regression**

Run: `mise exec -- go build ./... && mise exec -- go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/web/
git commit -m "$(printf '%s' '🏷️ Make web-UI tags clickable

The server already filters/browses by tag (store.ByTag, Filter.Tag) and the CLI
has --tag, but the web UI only displayed tags. Have /search read a tag param
(ByTag for tag-only browse, Filter.Tag through Hybrid with a query) and render
each result tag as an htmx link with a url-encoded value, so clicking a tag shows
everything with it and you can pivot by subject. No new search logic.')"
```

---

## Follow-up (out of scope for this plan)

A dedicated tag-cloud / "all my tags" page, an active-tag indicator with a clear button, and combining a clicked tag with the current text query all remain optional. Other tracked ideas: `--quiet`, `sync all` count dedup, tag normalization, a documented cron entry.
