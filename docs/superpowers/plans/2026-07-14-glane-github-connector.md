# GitHub Stars Connector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reusable sync framework (a persistent per-source cursor) and the first live connector — GitHub stars — reachable via `glane sync github`.

**Architecture:** A `sync_state` table with `GetCursor`/`SetCursor` helpers in `internal/store` is the whole shared framework. `internal/github` exposes `Sync(store, token, httpClient)` that pages `GET /user/starred` newest-first, upserts new stars, and advances the cursor only on success. `main.go` gains a `sync` subcommand reading `GITHUB_TOKEN`. Enrichment, search, and the web UI already handle any source with no change.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`, `strconv`, `time`), existing `modernc.org/sqlite` store. No new dependencies.

## Global Constraints

- Module path: `github.com/jcgay/glane` (verbatim in every import).
- Toolchain via mise (Go pinned in `mise.toml`); run go as `mise exec -- go ...`.
- No new dependencies — stdlib only.
- Auth via the `GITHUB_TOKEN` environment variable. Absent → a clear fatal error, never a panic or a silent empty sync.
- The cursor advances (`SetCursor`) ONLY after a fully successful run; any error returns before advancing it, so a re-run re-fetches idempotently (`store.Upsert` dedupes on `(source, source_id)`).
- `SourceID` for a star is the numeric repo **id** (stable across repo renames), stored as a string.
- Cursor values are RFC3339 UTC `starred_at` strings compared lexicographically.
- Commit messages: English, leading literal Unicode gitmoji character (not a `:shortcode:`), body explains *why*.

---

### Task 1: sync_state table + cursor helpers

**Files:**
- Modify: `internal/store/store.go` (add `sync_state` to the `schema` const)
- Create: `internal/store/sync.go`
- Test: `internal/store/sync_test.go`

**Interfaces:**
- Consumes: existing `Store` (unexported `db *sql.DB`).
- Produces:
  - `func (s *Store) GetCursor(source string) (string, error)` — returns `""` (no error) when the source has no row yet.
  - `func (s *Store) SetCursor(source, cursor string) error` — upserts the row, sets `updated_at` to now.

- [ ] **Step 1: Add the table to the schema**

In `internal/store/store.go`, append to the `schema` const string (after the `embeddings` table, before the closing backtick):
```sql

CREATE TABLE IF NOT EXISTS sync_state (
  source TEXT PRIMARY KEY,
  cursor TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL DEFAULT 0
);
```

- [ ] **Step 2: Write the failing test**

`internal/store/sync_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Unknown source → empty, no error.
	got, err := s.GetCursor("github")
	if err != nil || got != "" {
		t.Fatalf("want empty cursor, got %q err %v", got, err)
	}

	if err := s.SetCursor("github", "2023-01-15T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetCursor("github")
	if err != nil || got != "2023-01-15T10:00:00Z" {
		t.Fatalf("round-trip failed: got %q err %v", got, err)
	}

	// Overwrite.
	if err := s.SetCursor("github", "2024-02-20T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCursor("github")
	if got != "2024-02-20T00:00:00Z" {
		t.Fatalf("overwrite failed: got %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `mise exec -- go test ./internal/store/ -run Cursor`
Expected: FAIL — `undefined: (*Store).GetCursor`.

- [ ] **Step 4: Write `internal/store/sync.go`**

```go
package store

import "database/sql"

func (s *Store) GetCursor(source string) (string, error) {
	var cursor string
	err := s.db.QueryRow(`SELECT cursor FROM sync_state WHERE source = ?`, source).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cursor, nil
}

func (s *Store) SetCursor(source, cursor string) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_state (source, cursor, updated_at)
		VALUES (?, ?, strftime('%s','now'))
		ON CONFLICT(source) DO UPDATE SET
			cursor=excluded.cursor, updated_at=excluded.updated_at`,
		source, cursor)
	return err
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `mise exec -- go test ./internal/store/ -run Cursor`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/sync.go internal/store/sync_test.go
git commit -m "$(printf '%s' '🧭 Add per-source sync cursor to the store

Live sources accumulate new items continuously; a persistent cursor lets each
connector remember how far it got and fetch only what is new next time, instead
of re-paging its whole history. The cursor is an opaque per-source string so
every future connector (github now, mastodon/bluesky later) reuses one table.')"
```

---

### Task 2: GitHub connector

**Files:**
- Create: `internal/github/github.go`
- Test: `internal/github/github_test.go`

**Interfaces:**
- Consumes: `store.Item`, `store.Store` (`Upsert`, `GetCursor`, `SetCursor`), `store.Open` (in tests).
- Produces:
  - `func Sync(s *store.Store, token string, hc *http.Client) (int, error)` — returns the count of new stars imported.
  - Unexported `var perPage = 100` (page size; tests override it to force multi-page paths).
  - Unexported `var apiBase = "https://api.github.com"` (tests point it at an httptest server).

- [ ] **Step 1: Write the failing test**

`internal/github/github_test.go`:
```go
package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

// starPage renders n star entries as the star+json array, ids/timestamps
// derived from a starting index so ordering is newest-first (descending).
func starPage(startIdx, n int) string {
	out := "["
	for i := 0; i < n; i++ {
		idx := startIdx - i // descending
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(`{"starred_at":"2023-01-%02dT00:00:00Z","repo":{"id":%d,"full_name":"o/r%d","description":"d%d","html_url":"https://github.com/o/r%d","owner":{"login":"o"}}}`,
			idx, 1000+idx, idx, idx, idx)
	}
	return out + "]"
}

func TestSyncBackfillPaginatesAndMaps(t *testing.T) {
	perPage = 2
	defer func() { perPage = 100 }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			fmt.Fprint(w, starPage(31, 2)) // days 31,30 — a full page
		case "2":
			fmt.Fprint(w, starPage(29, 1)) // day 29 — partial page, ends paging
		default:
			fmt.Fprint(w, "[]")
		}
	}))
	defer srv.Close()
	apiBase = srv.URL
	defer func() { apiBase = "https://api.github.com" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	n, err := Sync(s, "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3 imported, got %d", n)
	}

	// Field mapping: search finds a repo by its description; source is github.
	res, _ := s.SearchFTS("d31", store.Filter{})
	if len(res) != 1 || res[0].Source != "github" || res[0].Kind != "star" {
		t.Fatalf("bad mapping: %+v", res)
	}
	// SourceID is the numeric repo id as a string.
	if res[0].SourceID != strconv.Itoa(1000+31) {
		t.Fatalf("want repo-id SourceID, got %q", res[0].SourceID)
	}

	// Cursor advanced to the newest starred_at seen (day 31).
	cur, _ := s.GetCursor("github")
	if cur != "2023-01-31T00:00:00Z" {
		t.Fatalf("cursor not advanced correctly: %q", cur)
	}
}

func TestSyncIncrementalStopsAtCursor(t *testing.T) {
	perPage = 100
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, starPage(20, 3)) // days 20,19,18
		} else {
			fmt.Fprint(w, "[]")
		}
	}))
	defer srv.Close()
	apiBase = srv.URL
	defer func() { apiBase = "https://api.github.com" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	// Pretend we already synced up to day 19.
	s.SetCursor("github", "2023-01-19T00:00:00Z")

	n, err := Sync(s, "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 { // only day 20 is newer than the cursor
		t.Fatalf("want 1 new item past cursor, got %d", n)
	}
}

func TestSyncAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad creds", 401)
	}))
	defer srv.Close()
	apiBase = srv.URL
	defer func() { apiBase = "https://api.github.com" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if _, err := Sync(s, "tok", srv.Client()); err == nil {
		t.Fatal("expected auth error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `mise exec -- go test ./internal/github/`
Expected: FAIL — `undefined: Sync` / build error.

- [ ] **Step 3: Write `internal/github/github.go`**

```go
package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jcgay/glane/internal/store"
)

var (
	apiBase = "https://api.github.com"
	perPage = 100
)

type starEntry struct {
	StarredAt string `json:"starred_at"`
	Repo      struct {
		ID          int64  `json:"id"`
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Owner       struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repo"`
}

func toItem(e starEntry) store.Item {
	text := e.Repo.FullName
	if e.Repo.Description != "" {
		text += " — " + e.Repo.Description
	}
	var ts int64
	if t, err := time.Parse(time.RFC3339, e.StarredAt); err == nil {
		ts = t.Unix()
	}
	return store.Item{
		Source:    "github",
		SourceID:  strconv.FormatInt(e.Repo.ID, 10),
		Kind:      "star",
		Author:    e.Repo.Owner.Login,
		Text:      text,
		URL:       e.Repo.HTMLURL,
		CreatedAt: ts,
	}
}

// Sync pages the token owner's starred repos newest-first, upserts everything
// newer than the stored cursor, and advances the cursor only after success.
func Sync(s *store.Store, token string, hc *http.Client) (int, error) {
	cursor, err := s.GetCursor("github")
	if err != nil {
		return 0, err
	}

	var items []store.Item
	newest := cursor

	for page := 1; ; page++ {
		url := fmt.Sprintf("%s/user/starred?sort=created&direction=desc&per_page=%d&page=%d",
			apiBase, perPage, page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github.star+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return 0, fmt.Errorf("GitHub auth failed (check GITHUB_TOKEN)")
		}
		if resp.StatusCode != http.StatusOK {
			reset := resp.Header.Get("X-RateLimit-Reset")
			resp.Body.Close()
			return 0, fmt.Errorf("GitHub API status %d (rate-limit reset %s)", resp.StatusCode, reset)
		}
		var entries []starEntry
		derr := json.NewDecoder(resp.Body).Decode(&entries)
		resp.Body.Close()
		if derr != nil {
			return 0, derr
		}
		if len(entries) == 0 {
			break
		}

		stop := false
		for _, e := range entries {
			if cursor != "" && e.StarredAt <= cursor {
				stop = true
				break
			}
			if e.StarredAt > newest {
				newest = e.StarredAt
			}
			items = append(items, toItem(e))
		}
		if stop || len(entries) < perPage {
			break
		}
	}

	if len(items) > 0 {
		if _, err := s.Upsert(items); err != nil {
			return 0, err
		}
	}
	if newest != cursor {
		if err := s.SetCursor("github", newest); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/github/ -v`
Expected: PASS (TestSyncBackfillPaginatesAndMaps, TestSyncIncrementalStopsAtCursor, TestSyncAuthFailure).

- [ ] **Step 5: Commit**

```bash
git add internal/github/
git commit -m "$(printf '%s' '⭐ Add GitHub stars sync connector

Stars are the simplest live source (public API, one token), so they establish
the connector rhythm the later ones reuse: page newest-first, stop at the
cursor, upsert, advance the cursor only on success. Repo id (not full name) is
the dedup key so a renamed repo is not re-imported as a new item, and starred_at
becomes created_at so --since and recency work for stars too.')"
```

---

### Task 3: `sync` subcommand + GITHUB_TOKEN

**Files:**
- Modify: `main.go` (add `sync` case + `cmdSync`)

**Interfaces:**
- Consumes: `github.Sync`, existing `store.Store`, `fatal`, and the existing embeddings/enrich HTTP client pattern.
- Produces: `glane sync github` — reads `GITHUB_TOKEN`, runs the connector, prints `synced N new stars`.

- [ ] **Step 1: Add the `sync` case to the dispatch switch**

In `main.go`, in the `switch os.Args[1]` block (alongside `import`/`search`/`serve`/`enrich`):
```go
	case "sync":
		cmdSync(s, os.Args[2:])
```

- [ ] **Step 2: Add `cmdSync`**

Add to `main.go` (imports: `"github.com/jcgay/glane/internal/github"`, and `"net/http"` and `"time"` if not already imported):
```go
func cmdSync(s *store.Store, args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("usage: glane sync <github>"))
	}
	switch args[0] {
	case "github":
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			fatal(fmt.Errorf("set GITHUB_TOKEN to sync GitHub stars"))
		}
		n, err := github.Sync(s, token, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new stars\n", n)
	default:
		fatal(fmt.Errorf("unknown sync source %q (known: github)", args[0]))
	}
}
```

- [ ] **Step 3: Update the top-level usage string**

In `main.go`, change the top-level usage line to include `sync`:
```go
	fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|serve> ...")
```

- [ ] **Step 4: Build and verify the command surface**

Run:
```bash
mise exec -- go build -o /tmp/glane . && /tmp/glane sync 2>&1; /tmp/glane sync bogus 2>&1
```
Expected: first prints the `usage: glane sync <github>` error; second prints `unknown sync source "bogus" (known: github)`. (Both exit non-zero — that's correct.)

- [ ] **Step 5: Verify the missing-token path**

Run:
```bash
env -u GITHUB_TOKEN /tmp/glane sync github 2>&1
```
Expected: `glane: set GITHUB_TOKEN to sync GitHub stars` (exit non-zero). Do NOT run a real authenticated sync in this step — network + real credentials are out of scope for automated verification.

- [ ] **Step 6: Full regression**

Run: `mise exec -- go build ./... && mise exec -- go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 7: Commit**

```bash
git add main.go
git commit -m "$(printf '%s' '🔌 Wire the glane sync github subcommand

Exposes the connector on the CLI and fails fast with a clear message when
GITHUB_TOKEN is unset, rather than making a doomed unauthenticated call. Uses
the conventional GITHUB_TOKEN name so an already-exported token (gh CLI, CI)
just works.')"
```

---

## Follow-up (out of scope for this plan)

Mastodon and Bluesky connectors reuse `sync_state`/`GetCursor`/`SetCursor` and add their own `internal/<source>` package + `sync <source>` case. Once a second connector exists, add `sync all` (iterate the known connectors). The optional LLM article-summary step remains a separate sub-project.
