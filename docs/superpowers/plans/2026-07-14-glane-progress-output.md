# Progress Output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show live progress on stderr while `sync`, `enrich`, and `summarize` run — scrolling one-line updates, no TTY tricks, no logic change.

**Architecture:** An optional variadic `progress ...func(string)` callback is added to the loop-bearing library functions (`github.Sync`, `mastodon.Sync`, `bluesky.Sync`, `enrich.Run`). Each builds a no-op-safe `report` and calls it per page (sync) or per item (enrich). `summarize`'s loop already lives in `main.go`, so it emits directly. `main.go` wires a `stderrProgress` callback. Variadic = every existing test call compiles unchanged and stays silent.

**Tech Stack:** Go stdlib (`fmt`, `os`, `net/url`). No new dependencies.

## Global Constraints

- Module path `github.com/jcgay/glane` (verbatim in every import).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies — stdlib only.
- `progress` is a variadic optional param (`progress ...func(string)`) added as the LAST parameter, so existing callers/tests compile unchanged and run silent.
- Progress → **stderr**; the existing final summary lines stay on **stdout**, unchanged.
- Per-page granularity for sync (cumulative stream count), per-item for enrich/summarize (`[i/n]`). No `\r`, no TTY detection, no percentages for sync.
- No change to sync/enrich/summarize control flow — only added `report(...)` calls.
- Commits: English, leading literal Unicode gitmoji, body explains *why*.

## The `report` idiom (used verbatim in each function)

At the top of each function taking `progress ...func(string)`:
```go
report := func(string) {}
if len(progress) > 0 && progress[0] != nil {
	report = progress[0]
}
```

---

### Task 1: Progress in the three connectors

**Files:**
- Modify: `internal/github/github.go` (add `progress`; report per page)
- Modify: `internal/mastodon/mastodon.go` (add `progress`; `syncStream` gains `label`+`report`)
- Modify: `internal/bluesky/bluesky.go` (add `progress`; `syncStream` gains `label`+`report`)
- Test: add a progress test to each connector's `_test.go`

**Interfaces:**
- Produces (signatures gain a trailing variadic):
  - `func Sync(s *store.Store, token string, hc *http.Client, progress ...func(string)) (int, error)` (github)
  - `func Sync(s *store.Store, baseURL, token string, hc *http.Client, progress ...func(string)) (int, error)` (mastodon)
  - `func Sync(s *store.Store, handle, appPassword string, hc *http.Client, progress ...func(string)) (int, error)` (bluesky)

- [ ] **Step 1: Write the failing progress tests**

Add to `internal/github/github_test.go`:
```go
func TestSyncReportsProgress(t *testing.T) {
	perPage = 100
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, starPage(3, 2))
		} else {
			fmt.Fprint(w, "[]")
		}
	}))
	defer srv.Close()
	apiBase = srv.URL
	defer func() { apiBase = "https://api.github.com" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	var msgs []string
	if _, err := Sync(s, "tok", srv.Client(), func(m string) { msgs = append(msgs, m) }); err != nil {
		t.Fatal(err)
	}
	if !containsSubstr(msgs, "github: stars") {
		t.Fatalf("no stars progress: %v", msgs)
	}
}

func containsSubstr(msgs []string, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}
```
Add to `internal/mastodon/mastodon_test.go`:
```go
func TestSyncReportsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/favourites":
			fmt.Fprint(w, `[`+statusJSON(30)+`]`)
		case "/api/v1/accounts/verify_credentials":
			fmt.Fprint(w, `{"id":"1"}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	var msgs []string
	if _, err := Sync(s, srv.URL, "tok", srv.Client(), func(m string) { msgs = append(msgs, m) }); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "mastodon: favourites") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no favourites progress: %v", msgs)
	}
}
```
Add to `internal/bluesky/bluesky_test.go`:
```go
func TestSyncReportsProgress(t *testing.T) {
	srv := newServer(t, serverPages{likes: map[string]string{
		"": fmt.Sprintf(`{"feed":[%s]}`, post("aaa")),
	}})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	var msgs []string
	if _, err := Sync(s, "alice.bsky.social", "pw", srv.Client(), func(m string) { msgs = append(msgs, m) }); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "bluesky: likes") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no likes progress: %v", msgs)
	}
}
```
(Ensure `strings` is imported in each test file.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/github/ ./internal/mastodon/ ./internal/bluesky/ -run Progress`
Expected: FAIL — `Sync` doesn't accept the callback / no messages emitted (compile error on the extra arg until signatures change).

- [ ] **Step 3: github — add progress + per-page report**

In `internal/github/github.go`, change `Sync`'s signature to end with `progress ...func(string)` and add the `report` idiom at the top. After the per-page loop appends that page's entries to `items` (just before the `stop`/`len(entries) < perPage` break check), add:
```go
		report(fmt.Sprintf("github: stars… %d", len(items)))
```
So a running cumulative count is emitted after each fetched page.

- [ ] **Step 4: mastodon — thread label+report through `syncStream`**

In `internal/mastodon/mastodon.go`:
- Change `syncStream`'s signature to add two params: `func syncStream(s *store.Store, url, token, cursorKey string, hc *http.Client, mapItem func(status) store.Item, label string, report func(string)) (int, error)`. After the per-page inner loop appends items (just before `if stop { break }`), add:
```go
		report(fmt.Sprintf("%s… %d", label, len(items)))
```
- Change `Sync` to end with `progress ...func(string)`, build `report` via the idiom, and pass a label + `report` to each `syncStream` call:
  - favourites → label `"mastodon: favourites"`
  - bookmarks → label `"mastodon: bookmarks"`
  - authorfeed → label `"mastodon: my posts"`

- [ ] **Step 5: bluesky — thread label+report through `syncStream`**

In `internal/bluesky/bluesky.go`:
- Change `syncStream`'s signature to `func syncStream(s *store.Store, cursorKey string, fetch func(pageCursor string) ([]store.Item, string, error), label string, report func(string)) (int, error)`. After the per-page inner loop appends to `all` (just before `if stop || next == "" { break }`), add:
```go
		report(fmt.Sprintf("%s… %d", label, len(all)))
```
- Change `Sync` to end with `progress ...func(string)`, build `report`, and add `label` + `report` to each stream entry / `syncStream` call:
  - likes → `"bluesky: likes"`
  - bookmarks → `"bluesky: saved"`
  - authorfeed → `"bluesky: my posts"`
  Update the `streams` slice to carry a `label` field and pass it through.

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/github/ ./internal/mastodon/ ./internal/bluesky/ -v`
Expected: PASS — the three new progress tests plus all existing tests (which pass no callback → silent).

- [ ] **Step 7: Commit**

```bash
git add internal/github/ internal/mastodon/ internal/bluesky/
git commit -m "$(printf '%s' '📡 Emit per-page sync progress from the connectors

sync gave no feedback until the final count. Add an optional variadic progress
callback to each connector Sync (variadic so every existing caller/test compiles
unchanged and stays silent) and emit a cumulative per-stream count after each
fetched page — mastodon: favourites... 40, bluesky: likes... 100, etc. Wiring to
stderr happens in main; here the connectors just report.')"
```

---

### Task 2: Progress in `enrich.Run`

**Files:**
- Modify: `internal/enrich/enrich.go` (add `progress`; per-item report)
- Test: `internal/enrich/enrich_test.go` (progress test)

**Interfaces:**
- Produces: `func Run(s *store.Store, hc *http.Client, emb *embed.Client, limit int, progress ...func(string)) (int, int, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/enrich/enrich_test.go` (reuse the httptest pattern from `TestRunIsolatesFailures`):
```go
func TestRunReportsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>T</title></head><body><article><p>hello world content</p></article></body></html>`)
	}))
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "post", URL: srv.URL + "/a"}})

	var msgs []string
	if _, _, err := Run(s, srv.Client(), nil, 10, func(m string) { msgs = append(msgs, m) }); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "enrich [1/1]") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no enrich progress: %v", msgs)
	}
}
```
(Ensure `strings` is imported.)

- [ ] **Step 2: Run test to verify it fails**

Run: `mise exec -- go test ./internal/enrich/ -run Progress`
Expected: FAIL — `Run` doesn't accept the callback.

- [ ] **Step 3: Add progress to `enrich.Run`**

In `internal/enrich/enrich.go`:
- Add `"net/url"` to imports if not present.
- Change `Run`'s signature to end with `progress ...func(string)`; add the `report` idiom at the top.
- Inside the loop over pending items, after `link` is chosen (the `FirstURL(it.Text)` / `it.URL` block) and before the fetch, add:
```go
		host := link
		if u, perr := url.Parse(link); perr == nil && u.Host != "" {
			host = u.Host
		}
		report(fmt.Sprintf("enrich [%d/%d] %s…", i+1, len(items), host))
```
where `items` is the pending slice and `i` is the loop index. If the current loop is `for _, it := range items`, change it to `for i, it := range items` so the index is available.

- [ ] **Step 4: Run test to verify it passes**

Run: `mise exec -- go test ./internal/enrich/ -v`
Expected: PASS (progress test + existing `TestRunIsolatesFailures` etc., which pass no callback).

- [ ] **Step 5: Commit**

```bash
git add internal/enrich/
git commit -m "$(printf '%s' '📡 Emit per-item enrich progress

Enriching does slow network fetches with no feedback until the end. Emit a
per-item line (enrich [i/n] host...) through an optional variadic progress
callback, so a long run shows which link it is on. Variadic keeps existing
callers/tests silent and unchanged.')"
```

---

### Task 3: Wire stderr progress in the CLI + summarize

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: the new variadic `progress` params on `github.Sync`/`mastodon.Sync`/`bluesky.Sync`/`enrich.Run`.
- Produces: `func stderrProgress(string)`; progress passed from `cmdSync`/`cmdSyncAll`/`cmdEnrich`; emitted in `cmdSummarize`'s loop.

- [ ] **Step 1: Add `stderrProgress` and pass it to the connectors + enrich**

In `main.go`, add:
```go
func stderrProgress(msg string) { fmt.Fprintln(os.Stderr, msg) }
```
Pass it as the trailing arg:
- `cmdSync` `github` case: `github.Sync(s, token, syncClient(), stderrProgress)`
- `cmdSync` `mastodon` case: `mastodon.Sync(s, base, token, syncClient(), stderrProgress)`
- `cmdSync` `bluesky` case: `bluesky.Sync(s, handle, pw, syncClient(), stderrProgress)`
- `cmdSyncAll`: add `stderrProgress` to each `github.Sync`/`mastodon.Sync`/`bluesky.Sync` call.
- `cmdEnrich`: add `stderrProgress` to the `enrich.Run(...)` call.

- [ ] **Step 2: Emit progress in `cmdSummarize`'s loop**

In `cmdSummarize`, change the loop to a counted loop and emit before each summarize call:
```go
	for i, it := range items {
		stderrProgress(fmt.Sprintf("summarize [%d/%d]…", i+1, len(items)))
		res, err := c.Summarize(context.Background(), it.ArticleTitle, it.ArticleText)
		...
	}
```
(Keep the rest of the loop — error log+continue, `SaveSummary`, counters — unchanged.)

- [ ] **Step 3: Build and manually verify progress appears on stderr**

Run:
```bash
mise exec -- go build -o /tmp/glane . && rm -f /tmp/glane-prog.db
GLANE_DB=/tmp/glane-prog.db /tmp/glane import twitter ./twitter >/dev/null
# summarize with no endpoint still fatals fast; instead exercise enrich progress on a couple items:
GLANE_DB=/tmp/glane-prog.db /tmp/glane enrich --limit 2 2>/tmp/glane-prog.err; echo "exit=$?"
echo "--- stderr had progress? ---"; grep -c 'enrich \[' /tmp/glane-prog.err
```
Expected: `enrich [1/2] …` / `enrich [2/2] …` lines captured in stderr (grep count ≥ 1); the final `enriched N, failed M` summary printed to stdout. (Network fetches may fail — that's fine; progress still prints per item regardless of fetch outcome.)

- [ ] **Step 4: Full regression**

Run: `mise exec -- go build ./... && mise exec -- go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "$(printf '%s' '📡 Wire progress output to stderr for sync/enrich/summarize

Give the three long commands live feedback: main passes a stderr-writing
callback to each connector Sync and to enrich.Run, and cmdSummarize emits a
per-item line in its own loop. Progress goes to stderr so the final summary on
stdout stays clean for piping and cron.')"
```

---

## Follow-up (out of scope for this plan)

A `--quiet` flag to suppress progress, and the earlier-noted `sync all` count dedup, remain optional polish. A live TTY counter was explicitly rejected in favor of scrolling lines.
