# Mastodon & Bluesky Connectors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `glane sync mastodon` (favourites + bookmarks) and `glane sync bluesky` (likes), plus `glane sync all`, reusing the existing sync-cursor framework.

**Architecture:** Two new packages, each a `Sync` function mirroring `internal/github`. `internal/mastodon` pages two streams via the `Link` header and stops at a status id ≤ the stored cursor. `internal/bluesky` authenticates a session, pages `getActorLikes`, and stops at a known post URI. Both reuse `store.GetCursor`/`SetCursor` with per-stream keys and `store.Upsert` for idempotent dedup. `main.go` grows `sync mastodon`/`sync bluesky`/`sync all`. Search, enrichment, and the web UI already handle any source.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`, `regexp`, `strings`, `html`, `strconv`, `time`), existing `store`. No new dependencies.

## Global Constraints

- Module path: `github.com/jcgay/glane` (verbatim in every import).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies — stdlib only. Do NOT add `golang.org/x/net/html`.
- Config via env vars: Mastodon `GLANE_MASTODON_URL` + `GLANE_MASTODON_TOKEN`; Bluesky `GLANE_BLUESKY_HANDLE` + `GLANE_BLUESKY_APP_PASSWORD`. Missing config → clear fatal, never a panic or doomed call.
- Cursor keys are per-stream: `mastodon:favourites`, `mastodon:bookmarks`, `bluesky:likes`. The stored item `Source` stays `mastodon`/`bluesky`.
- Each cursor advances only after that stream's run fully succeeds; any error returns before advancing it. `store.Upsert` dedups on `(source, source_id)`, so a re-run is idempotent.
- Mastodon `Link`-header pagination ids are opaque — follow the pre-built `next` URL, never construct `min_id` yourself. Incremental stop uses the exposed `status.id` (numeric compare).
- Tests are httptest-only — never hit the real Mastodon/Bluesky network.
- Commit messages: English, leading literal Unicode gitmoji character (not a `:shortcode:`), body explains *why*.

---

### Task 1: Mastodon connector

**Files:**
- Create: `internal/mastodon/mastodon.go`
- Test: `internal/mastodon/mastodon_test.go`

**Interfaces:**
- Consumes: `store.Item`, `store.Store` (`Upsert`, `GetCursor`, `SetCursor`, `SearchFTS`, `Open` in tests).
- Produces:
  - `func Sync(s *store.Store, baseURL, token string, hc *http.Client) (int, error)` — syncs favourites then bookmarks, returns total imported.
  - Unexported helpers `stripHTML(string) string`, `nextLink(linkHeader string) string`, `idNewer(a, cursor string) bool`.

- [ ] **Step 1: Write the failing test**

`internal/mastodon/mastodon_test.go`:
```go
package mastodon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestStripHTMLKeepsURLsDropsTags(t *testing.T) {
	// Mastodon splits a link's URL across spans; removing tags must rejoin it.
	in := `<p>Check <span class="invisible">https://</span><span>example.com/a</span></p><p>next</p>`
	got := stripHTML(in)
	if got != "Check https://example.com/a next" {
		t.Fatalf("got %q", got)
	}
	if stripHTML(`<p>caf&#233;</p>`) != "café" {
		t.Fatalf("entity not decoded: %q", stripHTML(`<p>caf&#233;</p>`))
	}
}

func TestSyncBothStreamsPaginateAndMap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/favourites":
			if r.URL.Query().Get("max_id") == "" {
				// page 1: ids 30,29 + a Link to page 2 (opaque pre-built URL)
				w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/favourites?max_id=OPAQUE>; rel="next"`, srvURL(r)))
				fmt.Fprint(w, `[`+statusJSON(30)+`,`+statusJSON(29)+`]`)
			} else {
				fmt.Fprint(w, `[`+statusJSON(28)+`]`) // page 2, no Link
			}
		case "/api/v1/bookmarks":
			fmt.Fprint(w, `[`+statusJSON(50)+`]`)
		default:
			http.Error(w, "no", 404)
		}
	}))
	defer srv.Close()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	n, err := Sync(s, srv.URL, "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 { // favourites 30,29,28 + bookmarks 50
		t.Fatalf("want 4 imported, got %d", n)
	}
	// kind distinction + mapping via search.
	fav, _ := s.SearchFTS("text30", store.Filter{})
	if len(fav) != 1 || fav[0].Kind != "like" || fav[0].Source != "mastodon" {
		t.Fatalf("favourite mapping wrong: %+v", fav)
	}
	bm, _ := s.SearchFTS("text50", store.Filter{})
	if len(bm) != 1 || bm[0].Kind != "bookmark" {
		t.Fatalf("bookmark mapping wrong: %+v", bm)
	}
	if cur, _ := s.GetCursor("mastodon:favourites"); cur != "30" {
		t.Fatalf("favourites cursor = %q, want 30", cur)
	}
	if cur, _ := s.GetCursor("mastodon:bookmarks"); cur != "50" {
		t.Fatalf("bookmarks cursor = %q, want 50", cur)
	}
}

func TestSyncIncrementalStopsAtStatusID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/favourites" {
			fmt.Fprint(w, `[`+statusJSON(20)+`,`+statusJSON(19)+`,`+statusJSON(18)+`]`)
		} else {
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.SetCursor("mastodon:favourites", "19")

	n, err := Sync(s, srv.URL, "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 { // only id 20 is newer than cursor 19
		t.Fatalf("want 1, got %d", n)
	}
}

func TestSyncAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", 401)
	}))
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if _, err := Sync(s, srv.URL, "tok", srv.Client()); err == nil {
		t.Fatal("expected auth error")
	}
}

// helpers for building fixture JSON
func statusJSON(id int) string {
	return fmt.Sprintf(`{"id":"%d","created_at":"2023-01-01T00:00:00Z","url":"https://m/@u/%d","content":"<p>text%d</p>","account":{"acct":"u@m"}}`, id, id, id)
}
func srvURL(r *http.Request) string { return "http://" + r.Host }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `mise exec -- go test ./internal/mastodon/`
Expected: FAIL — `undefined: Sync` / build error.

- [ ] **Step 3: Write `internal/mastodon/mastodon.go`**

```go
package mastodon

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/store"
)

var (
	// Block-level breaks become whitespace so words don't merge across
	// paragraphs; all other tags are removed with NO substitution so a URL
	// split across <span>s rejoins intact.
	breakRe = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>`)
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	wsRe    = regexp.MustCompile(`\s+`)
	nextRe  = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)
)

func stripHTML(s string) string {
	s = breakRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func nextLink(linkHeader string) string {
	if m := nextRe.FindStringSubmatch(linkHeader); m != nil {
		return m[1]
	}
	return ""
}

// idNewer reports whether Mastodon status id `a` is newer than the cursor.
// Ids are stringified snowflake integers → numeric compare when both parse;
// otherwise stop only on an exact match (treat everything else as newer).
func idNewer(a, cursor string) bool {
	if cursor == "" {
		return true
	}
	ai, aerr := strconv.ParseInt(a, 10, 64)
	ci, cerr := strconv.ParseInt(cursor, 10, 64)
	if aerr == nil && cerr == nil {
		return ai > ci
	}
	return a != cursor
}

type status struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
	Content   string `json:"content"`
	Account   struct {
		Acct string `json:"acct"`
	} `json:"account"`
}

func toItem(st status, kind string) store.Item {
	var ts int64
	if t, err := time.Parse(time.RFC3339, st.CreatedAt); err == nil {
		ts = t.Unix()
	}
	return store.Item{
		Source:    "mastodon",
		SourceID:  st.ID,
		Kind:      kind,
		Author:    st.Account.Acct,
		Text:      stripHTML(st.Content),
		URL:       st.URL,
		CreatedAt: ts,
	}
}

func syncStream(s *store.Store, url, token, kind, cursorKey string, hc *http.Client) (int, error) {
	cursor, err := s.GetCursor(cursorKey)
	if err != nil {
		return 0, err
	}
	var items []store.Item
	newest := cursor

	for url != "" {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return 0, fmt.Errorf("Mastodon auth failed (check GLANE_MASTODON_TOKEN)")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, fmt.Errorf("Mastodon API status %d for %s", resp.StatusCode, cursorKey)
		}
		var statuses []status
		next := nextLink(resp.Header.Get("Link"))
		derr := json.NewDecoder(resp.Body).Decode(&statuses)
		resp.Body.Close()
		if derr != nil {
			return 0, derr
		}
		if len(statuses) == 0 {
			break
		}
		stop := false
		for _, st := range statuses {
			if !idNewer(st.ID, cursor) {
				stop = true
				break
			}
			if idNewer(st.ID, newest) {
				newest = st.ID
			}
			items = append(items, toItem(st, kind))
		}
		if stop {
			break
		}
		url = next
	}

	if len(items) > 0 {
		if _, err := s.Upsert(items); err != nil {
			return 0, err
		}
	}
	if newest != cursor {
		if err := s.SetCursor(cursorKey, newest); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}

// Sync imports Mastodon favourites (kind "like") and bookmarks (kind "bookmark").
func Sync(s *store.Store, baseURL, token string, hc *http.Client) (int, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	fav, err := syncStream(s, baseURL+"/api/v1/favourites?limit=40", token, "like", "mastodon:favourites", hc)
	if err != nil {
		return fav, err
	}
	bm, err := syncStream(s, baseURL+"/api/v1/bookmarks?limit=40", token, "bookmark", "mastodon:bookmarks", hc)
	if err != nil {
		return fav + bm, err
	}
	return fav + bm, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/mastodon/ -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add internal/mastodon/
git commit -m "$(printf '%s' '🐘 Add Mastodon favourites + bookmarks connector

Mastodon is two independent streams, each with its own cursor. The Link-header
pagination ids are opaque internal Favourite ids, so we follow the pre-built
next URLs and stop instead at an exposed status.id <= cursor. Content is HTML;
tags are stripped without substitution so a link URL split across Mastodon spans
rejoins intact for enrich to follow, while block breaks become spaces.')"
```

---

### Task 2: Bluesky connector

**Files:**
- Create: `internal/bluesky/bluesky.go`
- Test: `internal/bluesky/bluesky_test.go`

**Interfaces:**
- Consumes: `store.Item`, `store.Store` (`Upsert`, `GetCursor`, `SetCursor`, `SearchFTS`, `Open` in tests).
- Produces:
  - `func Sync(s *store.Store, handle, appPassword string, hc *http.Client) (int, error)`.
  - Unexported `var pdsBase = "https://bsky.social"` (tests override to an httptest server), and helpers `createSession`, `permalink`.

- [ ] **Step 1: Write the failing test**

`internal/bluesky/bluesky_test.go`:
```go
package bluesky

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

// post builds a feedViewPost fixture; rkey doubles as the distinctive text word.
func post(rkey string) string {
	return fmt.Sprintf(`{"post":{"uri":"at://did:plc:abc/app.bsky.feed.post/%s","cid":"c","author":{"handle":"alice.bsky.social","did":"did:plc:abc"},"record":{"text":"word%s","createdAt":"2023-05-01T00:00:00Z"},"indexedAt":"2023-05-01T00:00:00Z"}}`, rkey, rkey)
}

func newServer(t *testing.T, pages map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/com.atproto.server.createSession":
			fmt.Fprint(w, `{"accessJwt":"jwt","refreshJwt":"r","handle":"alice.bsky.social","did":"did:plc:abc"}`)
		case "/xrpc/app.bsky.feed.getActorLikes":
			if body, ok := pages[r.URL.Query().Get("cursor")]; ok {
				fmt.Fprint(w, body)
			} else {
				fmt.Fprint(w, `{"feed":[]}`)
			}
		default:
			http.Error(w, "no", 404)
		}
	}))
}

func TestSyncAuthThenLikesPaginate(t *testing.T) {
	srv := newServer(t, map[string]string{
		"":   fmt.Sprintf(`{"cursor":"c1","feed":[%s,%s]}`, post("aaa"), post("bbb")),
		"c1": fmt.Sprintf(`{"feed":[%s]}`, post("ccc")), // no cursor → last page
	})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	n, err := Sync(s, "alice.bsky.social", "app-pw", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3, got %d", n)
	}
	res, _ := s.SearchFTS("wordaaa", store.Filter{})
	if len(res) != 1 || res[0].Source != "bluesky" || res[0].Kind != "like" {
		t.Fatalf("mapping wrong: %+v", res)
	}
	if res[0].URL != "https://bsky.app/profile/alice.bsky.social/post/aaa" {
		t.Fatalf("permalink wrong: %q", res[0].URL)
	}
	// cursor = newest post URI (first seen).
	if cur, _ := s.GetCursor("bluesky:likes"); cur != "at://did:plc:abc/app.bsky.feed.post/aaa" {
		t.Fatalf("cursor wrong: %q", cur)
	}
}

func TestSyncStopsAtKnownURI(t *testing.T) {
	srv := newServer(t, map[string]string{
		"": fmt.Sprintf(`{"cursor":"c1","feed":[%s,%s]}`, post("new"), post("old")),
	})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.SetCursor("bluesky:likes", "at://did:plc:abc/app.bsky.feed.post/old")

	n, err := Sync(s, "alice.bsky.social", "app-pw", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 { // only "new" is imported; stops at the known "old" URI
		t.Fatalf("want 1, got %d", n)
	}
}

func TestSyncAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", 401)
	}))
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if _, err := Sync(s, "alice.bsky.social", "app-pw", srv.Client()); err == nil {
		t.Fatal("expected auth error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `mise exec -- go test ./internal/bluesky/`
Expected: FAIL — `undefined: Sync` / build error.

- [ ] **Step 3: Write `internal/bluesky/bluesky.go`**

```go
package bluesky

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/store"
)

var pdsBase = "https://bsky.social"

func createSession(handle, appPassword string, hc *http.Client) (string, error) {
	body, _ := json.Marshal(map[string]string{"identifier": handle, "password": appPassword})
	req, err := http.NewRequest("POST", pdsBase+"/xrpc/com.atproto.server.createSession", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Bluesky auth failed (check GLANE_BLUESKY_APP_PASSWORD)")
	}
	var out struct {
		AccessJwt string `json:"accessJwt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.AccessJwt, nil
}

type likesResp struct {
	Cursor string `json:"cursor"`
	Feed   []struct {
		Post struct {
			URI    string `json:"uri"`
			Author struct {
				Handle string `json:"handle"`
			} `json:"author"`
			Record struct {
				Text      string `json:"text"`
				CreatedAt string `json:"createdAt"`
			} `json:"record"`
		} `json:"post"`
	} `json:"feed"`
}

// permalink turns an AT-URI (at://did/app.bsky.feed.post/<rkey>) into a bsky.app link.
func permalink(handle, uri string) string {
	parts := strings.Split(uri, "/")
	rkey := parts[len(parts)-1]
	return "https://bsky.app/profile/" + handle + "/post/" + rkey
}

func Sync(s *store.Store, handle, appPassword string, hc *http.Client) (int, error) {
	jwt, err := createSession(handle, appPassword, hc)
	if err != nil {
		return 0, err
	}
	cursor, err := s.GetCursor("bluesky:likes")
	if err != nil {
		return 0, err
	}

	var items []store.Item
	newest := ""
	pageCursor := ""

	for {
		url := fmt.Sprintf("%s/xrpc/app.bsky.feed.getActorLikes?actor=%s&limit=100", pdsBase, handle)
		if pageCursor != "" {
			url += "&cursor=" + pageCursor
		}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		resp, err := hc.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, fmt.Errorf("Bluesky API status %d", resp.StatusCode)
		}
		var lr likesResp
		derr := json.NewDecoder(resp.Body).Decode(&lr)
		resp.Body.Close()
		if derr != nil {
			return 0, derr
		}
		if len(lr.Feed) == 0 {
			break
		}

		stop := false
		for _, f := range lr.Feed {
			p := f.Post
			if cursor != "" && p.URI == cursor {
				stop = true
				break
			}
			if newest == "" {
				newest = p.URI // newest-first: first seen this run is the newest
			}
			var ts int64
			if t, terr := time.Parse(time.RFC3339, p.Record.CreatedAt); terr == nil {
				ts = t.Unix()
			}
			items = append(items, store.Item{
				Source:    "bluesky",
				SourceID:  p.URI,
				Kind:      "like",
				Author:    p.Author.Handle,
				Text:      p.Record.Text,
				URL:       permalink(p.Author.Handle, p.URI),
				CreatedAt: ts,
			})
		}
		if stop || lr.Cursor == "" {
			break
		}
		pageCursor = lr.Cursor
	}

	if len(items) > 0 {
		if _, err := s.Upsert(items); err != nil {
			return 0, err
		}
	}
	if newest != "" && newest != cursor {
		if err := s.SetCursor("bluesky:likes", newest); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/bluesky/ -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/bluesky/
git commit -m "$(printf '%s' '🦋 Add Bluesky likes connector

Bluesky needs an app-password session (createSession → accessJwt) and has no
server-side "since", so getActorLikes is paged newest-first and stopped at the
post URI stored from the last run. The post URI is the stable dedup key and the
rkey builds a bsky.app permalink. Upsert idempotency covers the case where the
cursor post was unliked and the stop URI never appears.')"
```

---

### Task 3: `sync mastodon` / `sync bluesky` / `sync all`

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `mastodon.Sync`, `bluesky.Sync`, existing `github.Sync`, `store.Store`, `fatal`.
- Produces: `glane sync mastodon`, `glane sync bluesky`, `glane sync all`.

- [ ] **Step 1: Add a shared HTTP client helper and the new `cmdSync` cases**

In `main.go`, add imports `"github.com/jcgay/glane/internal/mastodon"` and `"github.com/jcgay/glane/internal/bluesky"` (and `"strings"` if not present). Add a helper and extend `cmdSync`'s switch (keep the existing `github` case):
```go
func syncClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }
```
Replace the `github` case's client with `syncClient()` for consistency, and add:
```go
	case "mastodon":
		base, token := os.Getenv("GLANE_MASTODON_URL"), os.Getenv("GLANE_MASTODON_TOKEN")
		if base == "" || token == "" {
			fatal(fmt.Errorf("set GLANE_MASTODON_URL and GLANE_MASTODON_TOKEN to sync Mastodon"))
		}
		n, err := mastodon.Sync(s, base, token, syncClient())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new mastodon items\n", n)
	case "bluesky":
		handle, pw := os.Getenv("GLANE_BLUESKY_HANDLE"), os.Getenv("GLANE_BLUESKY_APP_PASSWORD")
		if handle == "" || pw == "" {
			fatal(fmt.Errorf("set GLANE_BLUESKY_HANDLE and GLANE_BLUESKY_APP_PASSWORD to sync Bluesky"))
		}
		n, err := bluesky.Sync(s, handle, pw, syncClient())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("synced %d new bluesky items\n", n)
	case "all":
		cmdSyncAll(s)
```
Update the unknown-source error and usage strings to list `github`, `mastodon`, `bluesky`, `all`.

- [ ] **Step 2: Add `cmdSyncAll`**

```go
// cmdSyncAll runs every connector whose config is present, skipping the rest
// (reported, not errored). One connector's failure is logged but does not stop
// the others; each connector's own cursor only advances on its own success.
func cmdSyncAll(s *store.Store) {
	hc := syncClient()
	total := 0
	var ran, skipped []string

	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		if n, err := github.Sync(s, tok, hc); err != nil {
			fmt.Fprintf(os.Stderr, "glane: github sync error: %v\n", err)
		} else {
			total += n
			ran = append(ran, fmt.Sprintf("github:%d", n))
		}
	} else {
		skipped = append(skipped, "github")
	}

	if base, tok := os.Getenv("GLANE_MASTODON_URL"), os.Getenv("GLANE_MASTODON_TOKEN"); base != "" && tok != "" {
		if n, err := mastodon.Sync(s, base, tok, hc); err != nil {
			fmt.Fprintf(os.Stderr, "glane: mastodon sync error: %v\n", err)
		} else {
			total += n
			ran = append(ran, fmt.Sprintf("mastodon:%d", n))
		}
	} else {
		skipped = append(skipped, "mastodon")
	}

	if h, pw := os.Getenv("GLANE_BLUESKY_HANDLE"), os.Getenv("GLANE_BLUESKY_APP_PASSWORD"); h != "" && pw != "" {
		if n, err := bluesky.Sync(s, h, pw, hc); err != nil {
			fmt.Fprintf(os.Stderr, "glane: bluesky sync error: %v\n", err)
		} else {
			total += n
			ran = append(ran, fmt.Sprintf("bluesky:%d", n))
		}
	} else {
		skipped = append(skipped, "bluesky")
	}

	fmt.Printf("synced %d new items [%s]", total, strings.Join(ran, " "))
	if len(skipped) > 0 {
		fmt.Printf(" (skipped, not configured: %s)", strings.Join(skipped, ", "))
	}
	fmt.Println()
}
```

- [ ] **Step 3: Build and verify the command surface**

Run:
```bash
mise exec -- go build -o /tmp/glane . && /tmp/glane sync bogus 2>&1
env -u GLANE_MASTODON_URL -u GLANE_MASTODON_TOKEN /tmp/glane sync mastodon 2>&1
env -u GLANE_BLUESKY_HANDLE -u GLANE_BLUESKY_APP_PASSWORD /tmp/glane sync bluesky 2>&1
```
Expected: `unknown sync source "bogus" (known: github, mastodon, bluesky, all)`; then the two `set GLANE_...` fatal messages. (All exit non-zero — correct.)

- [ ] **Step 4: Verify `sync all` skips unconfigured connectors**

Run (clear all connector env so everything is skipped — must NOT error, just report):
```bash
env -u GITHUB_TOKEN -u GLANE_MASTODON_URL -u GLANE_MASTODON_TOKEN -u GLANE_BLUESKY_HANDLE -u GLANE_BLUESKY_APP_PASSWORD \
  GLANE_DB=/tmp/glane-all.db /tmp/glane sync all
```
Expected: `synced 0 new items [] (skipped, not configured: github, mastodon, bluesky)`, exit 0. Do NOT run a real authenticated sync.

- [ ] **Step 5: Full regression**

Run: `mise exec -- go build ./... && mise exec -- go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add main.go
git commit -m "$(printf '%s' '🔌 Wire sync mastodon, sync bluesky, and sync all

Exposes both connectors on the CLI with fail-fast config checks, and adds
sync all so a single scheduled command drives every configured source. sync all
skips unconfigured connectors (reported, not errored) and keeps going when one
fails, since each connector advances only its own cursor — a partial outage
never blocks the healthy sources.')"
```

---

## Follow-up (out of scope for this plan)

The optional LLM article-summary step remains a separate sub-project. A documented cron/launchd entry for `glane sync all` is a docs task once the connectors are in real use.
