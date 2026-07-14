# Bluesky Saves + My Posts/Reposts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the Bluesky connector with saved posts + my posts/reposts, and the Mastodon connector with my posts/boosts — each a new paginated stream, no schema change.

**Architecture:** Bluesky's `Sync` becomes an ordered list of streams (likes → bookmarks → authorfeed), each running a shared stop-at-URI loop with its own cursor and its own `Upsert`. Mastodon's `syncStream` is generalized to take a per-status mapper (so one status can become an "own" or "repost" item), and `Sync` adds an authorfeed stream after `verify_credentials`. Fixed stream order gives kind precedence (posts/reposts > bookmark > like) through the existing `Upsert` overwrite. No CLI change — `sync bluesky`/`sync mastodon`/`sync all` pick up the new streams automatically.

**Tech Stack:** Go stdlib (`net/http`, `net/url`, `encoding/json`, `strings`, `time`), existing `store`. No new dependencies.

## Global Constraints

- Module path `github.com/jcgay/glane` (verbatim in every import).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies — stdlib only.
- One row per `(source, source_id)`; NO schema or `Upsert` change. Kind precedence comes only from fixed stream order (like → bookmark → posts/reposts) + the existing `Upsert` `kind=excluded.kind` overwrite.
- Each new cursor (`bluesky:bookmarks`, `bluesky:authorfeed`, `mastodon:authorfeed`) advances only after its stream fully succeeds.
- Verified API facts: Bluesky `getBookmarks` → `{bookmarks:[{item(union), createdAt, subject}], cursor}`, `item.$type == "app.bsky.feed.defs#postView"` is the only case to keep; `getAuthorFeed?filter=posts_no_replies` → `{feed:[{post, reason?}], cursor}`, `reason.$type == "app.bsky.feed.defs#reasonRepost"` marks a repost. Mastodon `verify_credentials` → `{id}`; `accounts/{id}/statuses?exclude_replies=true` Link-paged, a status with non-null `reblog` is a boost.
- Replies excluded (`posts_no_replies` / `exclude_replies=true`). Reposts/boosts store the ORIGINAL post's fields with `kind:"repost"`.
- Tests httptest-only — never a real network.
- Commits: English, leading literal Unicode gitmoji, body explains *why*.

---

### Task 1: Bluesky — saved posts + my posts/reposts

**Files:**
- Rewrite: `internal/bluesky/bluesky.go` (refactor `Sync` into ordered streams; add bookmarks + authorfeed)
- Modify: `internal/bluesky/bluesky_test.go` (extend the test server for the new endpoints; add tests)

**Interfaces:**
- Consumes: `store.Item`, `store.Store` (`Upsert`, `GetCursor`, `SetCursor`, `SearchFTS`).
- Produces: unchanged public API — `func Sync(s *store.Store, handle, appPassword string, hc *http.Client) (int, error)` now also imports bookmarks (`kind:"bookmark"`) and author feed (`kind:"own"`/`"repost"`). Unexported `var pdsBase`, `createSession`, `permalink` stay.

- [ ] **Step 1: Update the test server + add failing tests**

Replace the `newServer` helper in `internal/bluesky/bluesky_test.go` so it also answers `getBookmarks` and `getAuthorFeed` (empty unless the test provides bodies), and keep the existing likes tests working:
```go
type serverPages struct {
	likes      map[string]string // keyed by request cursor
	bookmarks  map[string]string
	authorfeed map[string]string
}

func newServer(t *testing.T, p serverPages) *httptest.Server {
	page := func(m map[string]string, cursor, empty string) string {
		if m != nil {
			if body, ok := m[cursor]; ok {
				return body
			}
		}
		return empty
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := r.URL.Query().Get("cursor")
		switch r.URL.Path {
		case "/xrpc/com.atproto.server.createSession":
			fmt.Fprint(w, `{"accessJwt":"jwt","refreshJwt":"r","handle":"alice.bsky.social","did":"did:plc:abc"}`)
		case "/xrpc/app.bsky.feed.getActorLikes":
			fmt.Fprint(w, page(p.likes, c, `{"feed":[]}`))
		case "/xrpc/app.bsky.bookmark.getBookmarks":
			fmt.Fprint(w, page(p.bookmarks, c, `{"bookmarks":[]}`))
		case "/xrpc/app.bsky.feed.getAuthorFeed":
			fmt.Fprint(w, page(p.authorfeed, c, `{"feed":[]}`))
		default:
			http.Error(w, "no", 404)
		}
	}))
}

// feedPost renders a feedViewPost; rkey doubles as the distinctive text word.
func feedPost(rkey string) string {
	return fmt.Sprintf(`{"post":{"uri":"at://did:plc:abc/app.bsky.feed.post/%s","author":{"handle":"bob.bsky.social"},"record":{"text":"word%s","createdAt":"2023-05-01T00:00:00Z"}}}`, rkey, rkey)
}
```
Update the EXISTING likes tests to the new helper signature: in `TestSyncAuthThenLikesPaginate` and `TestSyncStopsAtKnownURI`, replace the `map[string]string{...}` argument with `serverPages{likes: map[string]string{...}}` (same body strings, now under `likes:`). Their assertions are unchanged.

Add new tests to `internal/bluesky/bluesky_test.go`:
```go
func TestSyncBookmarksSkipsUnviewable(t *testing.T) {
	srv := newServer(t, serverPages{bookmarks: map[string]string{
		"": `{"bookmarks":[
			{"createdAt":"2023-05-02T00:00:00Z","item":{"$type":"app.bsky.feed.defs#postView","uri":"at://did:plc:abc/app.bsky.feed.post/bk1","author":{"handle":"carol.bsky.social"},"record":{"text":"saved kubernetes post","createdAt":"2023-05-01T00:00:00Z"}}},
			{"createdAt":"2023-05-02T00:00:00Z","item":{"$type":"app.bsky.feed.defs#notFoundPost","uri":"at://x/y/z","notFound":true}}
		]}`,
	}})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	n, err := Sync(s, "alice.bsky.social", "pw", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 { // only the postView bookmark; notFoundPost skipped
		t.Fatalf("want 1 bookmark, got %d", n)
	}
	res, _ := s.SearchFTS("kubernetes", store.Filter{})
	if len(res) != 1 || res[0].Kind != "bookmark" || res[0].Source != "bluesky" {
		t.Fatalf("bad bookmark mapping: %+v", res)
	}
	if res[0].URL != "https://bsky.app/profile/carol.bsky.social/post/bk1" {
		t.Fatalf("permalink %q", res[0].URL)
	}
}

func TestSyncAuthorFeedOwnVsRepost(t *testing.T) {
	srv := newServer(t, serverPages{authorfeed: map[string]string{
		"": fmt.Sprintf(`{"feed":[%s,%s]}`,
			// own post (no reason)
			`{"post":{"uri":"at://did:plc:abc/app.bsky.feed.post/mine","author":{"handle":"alice.bsky.social"},"record":{"text":"my own thoughts on rust","createdAt":"2023-06-01T00:00:00Z"}}}`,
			// repost (reason = reasonRepost) of someone else's post
			`{"reason":{"$type":"app.bsky.feed.defs#reasonRepost"},"post":{"uri":"at://did:plc:xyz/app.bsky.feed.post/theirs","author":{"handle":"dan.bsky.social"},"record":{"text":"a great go article","createdAt":"2023-05-20T00:00:00Z"}}}`,
		),
	}})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if _, err := Sync(s, "alice.bsky.social", "pw", srv.Client()); err != nil {
		t.Fatal(err)
	}
	own, _ := s.SearchFTS("thoughts", store.Filter{})
	if len(own) != 1 || own[0].Kind != "own" {
		t.Fatalf("own post wrong: %+v", own)
	}
	rep, _ := s.SearchFTS("article", store.Filter{})
	if len(rep) != 1 || rep[0].Kind != "repost" || rep[0].Author != "dan.bsky.social" {
		t.Fatalf("repost wrong: %+v", rep)
	}
}

func TestSyncKindPrecedenceRepostOverLike(t *testing.T) {
	uri := "at://did:plc:xyz/app.bsky.feed.post/shared"
	post := fmt.Sprintf(`{"post":{"uri":"%s","author":{"handle":"dan.bsky.social"},"record":{"text":"shared thing","createdAt":"2023-05-01T00:00:00Z"}}}`, uri)
	srv := newServer(t, serverPages{
		likes:      map[string]string{"": fmt.Sprintf(`{"feed":[%s]}`, post)},
		authorfeed: map[string]string{"": fmt.Sprintf(`{"feed":[{"reason":{"$type":"app.bsky.feed.defs#reasonRepost"},%s]}`, post[1:])},
	})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if _, err := Sync(s, "alice.bsky.social", "pw", srv.Client()); err != nil {
		t.Fatal(err)
	}
	res, _ := s.SearchFTS("shared", store.Filter{})
	// same uri liked AND reposted → one row; authorfeed runs last → repost wins
	if len(res) != 1 || res[0].Kind != "repost" {
		t.Fatalf("precedence failed, want single repost, got %+v", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/bluesky/`
Expected: FAIL — the new endpoints aren't fetched yet (compile error on `serverPages` or failing assertions).

- [ ] **Step 3: Rewrite `internal/bluesky/bluesky.go`**

```go
package bluesky

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		return "", fmt.Errorf("Bluesky auth failed (check BLUESKY_APP_PASSWORD)")
	}
	var out struct {
		AccessJwt string `json:"accessJwt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.AccessJwt, nil
}

// postView is the common Bluesky post shape across likes/bookmarks/author-feed.
type postView struct {
	URI    string `json:"uri"`
	Author struct {
		Handle string `json:"handle"`
	} `json:"author"`
	Record struct {
		Text      string `json:"text"`
		CreatedAt string `json:"createdAt"`
	} `json:"record"`
}

func (p postView) toItem(kind string) store.Item {
	var ts int64
	if t, err := time.Parse(time.RFC3339, p.Record.CreatedAt); err == nil {
		ts = t.Unix()
	}
	return store.Item{
		Source:    "bluesky",
		SourceID:  p.URI,
		Kind:      kind,
		Author:    p.Author.Handle,
		Text:      p.Record.Text,
		URL:       permalink(p.Author.Handle, p.URI),
		CreatedAt: ts,
	}
}

func permalink(handle, uri string) string {
	parts := strings.Split(uri, "/")
	rkey := parts[len(parts)-1]
	return "https://bsky.app/profile/" + handle + "/post/" + rkey
}

func getJSON(hc *http.Client, jwt, reqURL string, out any) error {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bluesky API status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// syncStream runs the stop-at-URI + upsert + cursor-advance loop for one stream.
// fetch(pageCursor) returns the page's items (newest-first; each item's SourceID
// is its post URI, used as the stop key) and the next page cursor ("" = end).
func syncStream(s *store.Store, cursorKey string, fetch func(pageCursor string) ([]store.Item, string, error)) (int, error) {
	cursor, err := s.GetCursor(cursorKey)
	if err != nil {
		return 0, err
	}
	var all []store.Item
	newest := ""
	pageCursor := ""
	for {
		items, next, err := fetch(pageCursor)
		if err != nil {
			return 0, err
		}
		if len(items) == 0 {
			break
		}
		stop := false
		for _, it := range items {
			if cursor != "" && it.SourceID == cursor {
				stop = true
				break
			}
			if newest == "" {
				newest = it.SourceID
			}
			all = append(all, it)
		}
		if stop || next == "" {
			break
		}
		pageCursor = next
	}
	if len(all) > 0 {
		if _, err := s.Upsert(all); err != nil {
			return 0, err
		}
	}
	if newest != "" && newest != cursor {
		if err := s.SetCursor(cursorKey, newest); err != nil {
			return len(all), err
		}
	}
	return len(all), nil
}

func likesFetch(hc *http.Client, jwt, handle string) func(string) ([]store.Item, string, error) {
	return func(pageCursor string) ([]store.Item, string, error) {
		u := fmt.Sprintf("%s/xrpc/app.bsky.feed.getActorLikes?actor=%s&limit=100", pdsBase, url.QueryEscape(handle))
		if pageCursor != "" {
			u += "&cursor=" + url.QueryEscape(pageCursor)
		}
		var out struct {
			Cursor string `json:"cursor"`
			Feed   []struct {
				Post postView `json:"post"`
			} `json:"feed"`
		}
		if err := getJSON(hc, jwt, u, &out); err != nil {
			return nil, "", err
		}
		items := make([]store.Item, 0, len(out.Feed))
		for _, f := range out.Feed {
			items = append(items, f.Post.toItem("like"))
		}
		return items, out.Cursor, nil
	}
}

func bookmarksFetch(hc *http.Client, jwt string) func(string) ([]store.Item, string, error) {
	return func(pageCursor string) ([]store.Item, string, error) {
		u := fmt.Sprintf("%s/xrpc/app.bsky.bookmark.getBookmarks?limit=100", pdsBase)
		if pageCursor != "" {
			u += "&cursor=" + url.QueryEscape(pageCursor)
		}
		var out struct {
			Cursor    string `json:"cursor"`
			Bookmarks []struct {
				Item struct {
					Type string `json:"$type"`
					postView
				} `json:"item"`
			} `json:"bookmarks"`
		}
		if err := getJSON(hc, jwt, u, &out); err != nil {
			return nil, "", err
		}
		items := make([]store.Item, 0, len(out.Bookmarks))
		for _, b := range out.Bookmarks {
			if b.Item.Type != "app.bsky.feed.defs#postView" {
				continue // blockedPost / notFoundPost — not viewable
			}
			items = append(items, b.Item.postView.toItem("bookmark"))
		}
		return items, out.Cursor, nil
	}
}

func authorFeedFetch(hc *http.Client, jwt, handle string) func(string) ([]store.Item, string, error) {
	return func(pageCursor string) ([]store.Item, string, error) {
		u := fmt.Sprintf("%s/xrpc/app.bsky.feed.getAuthorFeed?actor=%s&limit=100&filter=posts_no_replies", pdsBase, url.QueryEscape(handle))
		if pageCursor != "" {
			u += "&cursor=" + url.QueryEscape(pageCursor)
		}
		var out struct {
			Cursor string `json:"cursor"`
			Feed   []struct {
				Post   postView `json:"post"`
				Reason struct {
					Type string `json:"$type"`
				} `json:"reason"`
			} `json:"feed"`
		}
		if err := getJSON(hc, jwt, u, &out); err != nil {
			return nil, "", err
		}
		items := make([]store.Item, 0, len(out.Feed))
		for _, f := range out.Feed {
			kind := "own"
			if f.Reason.Type == "app.bsky.feed.defs#reasonRepost" {
				kind = "repost"
			}
			items = append(items, f.Post.toItem(kind))
		}
		return items, out.Cursor, nil
	}
}

// Sync imports likes, then saved posts (bookmarks), then the author's own posts
// and reposts. Order matters: later streams win kind on a shared post URI
// (repost/own > bookmark > like) via Upsert's overwrite.
func Sync(s *store.Store, handle, appPassword string, hc *http.Client) (int, error) {
	jwt, err := createSession(handle, appPassword, hc)
	if err != nil {
		return 0, err
	}
	streams := []struct {
		key   string
		fetch func(string) ([]store.Item, string, error)
	}{
		{"bluesky:likes", likesFetch(hc, jwt, handle)},
		{"bluesky:bookmarks", bookmarksFetch(hc, jwt)},
		{"bluesky:authorfeed", authorFeedFetch(hc, jwt, handle)},
	}
	total := 0
	for _, st := range streams {
		n, err := syncStream(s, st.key, st.fetch)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/bluesky/ -v`
Expected: PASS — existing likes tests (adapted to `serverPages`) plus the three new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/bluesky/
git commit -m "$(printf '%s' '🦋 Sync Bluesky saved posts and my posts/reposts

Likes were only part of the Bluesky footprint. Add saved posts (getBookmarks,
skipping blocked/not-found items) and the author feed (getAuthorFeed) split into
own posts vs reposts by the reasonRepost marker. Sync is refactored into ordered
streams sharing one stop-at-URI loop, each with its own cursor and Upsert; the
fixed order (likes -> bookmarks -> author feed) makes the strongest relationship
win kind on a post touched by several, with no schema change.')"
```

---

### Task 2: Mastodon — my posts + boosts

**Files:**
- Modify: `internal/mastodon/mastodon.go` (generalize `syncStream` to a per-status mapper; add `verifyCredentials`; add the authorfeed stream to `Sync`; add `Reblog` to the `status` struct)
- Modify: `internal/mastodon/mastodon_test.go` (extend existing test servers for the new endpoints; add a test)

**Interfaces:**
- Consumes: `store.Item`, `store.Store`.
- Produces: unchanged public API — `func Sync(s *store.Store, baseURL, token string, hc *http.Client) (int, error)` now also imports own posts (`kind:"own"`) and boosts (`kind:"repost"`).

- [ ] **Step 1: Extend existing test servers + add a failing test**

Every existing test in `mastodon_test.go` calls `Sync`, which will now also hit `verify_credentials` and `accounts/{id}/statuses`. Update each existing test's httptest handler to answer those so the current assertions still hold:
- Add a `case "/api/v1/accounts/verify_credentials":` returning `{"id":"1"}`.
- Ensure the account-statuses path (`/api/v1/accounts/1/statuses`) returns `[]` — in handlers that already `fmt.Fprint(w, "[]")` for unknown paths this is automatic; in `TestSyncBothStreamsPaginateAndMap` (which `switch`es on path) add a `default:`/statuses case returning `[]`.

Add this test:
```go
func TestSyncAuthorFeedOwnVsBoost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			fmt.Fprint(w, `{"id":"7"}`)
		case r.URL.Path == "/api/v1/accounts/7/statuses":
			fmt.Fprint(w, `[
				{"id":"200","created_at":"2023-01-01T00:00:00Z","url":"https://m/@me/200","content":"<p>my own note about rust</p>","account":{"acct":"me@m"}},
				{"id":"199","created_at":"2023-01-01T00:00:00Z","url":"https://m/@me/199","content":"","account":{"acct":"me@m"},"reblog":{"id":"5001","created_at":"2022-12-01T00:00:00Z","url":"https://other/@bob/5001","content":"<p>bob on kubernetes</p>","account":{"acct":"bob@other"}}}
			]`)
		default:
			fmt.Fprint(w, `[]`) // favourites + bookmarks empty
		}
	}))
	defer srv.Close()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	n, err := Sync(s, srv.URL, "tok", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 (own + boost), got %d", n)
	}
	own, _ := s.SearchFTS("note", store.Filter{})
	if len(own) != 1 || own[0].Kind != "own" {
		t.Fatalf("own wrong: %+v", own)
	}
	// boost maps to the reblogged status (its id, author, content, url)
	boost, _ := s.SearchFTS("kubernetes", store.Filter{})
	if len(boost) != 1 || boost[0].Kind != "repost" || boost[0].SourceID != "5001" ||
		boost[0].Author != "bob@other" || boost[0].URL != "https://other/@bob/5001" {
		t.Fatalf("boost wrong: %+v", boost)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/mastodon/`
Expected: FAIL — `Reblog` field / authorfeed not implemented; new test fails.

- [ ] **Step 3: Generalize `syncStream` and add the authorfeed stream**

In `internal/mastodon/mastodon.go`:

Add `Reblog` to the `status` struct (self-referential pointer):
```go
type status struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
	Content   string `json:"content"`
	Account   struct {
		Acct string `json:"acct"`
	} `json:"account"`
	Reblog *status `json:"reblog"`
}
```

Change `syncStream` to take a per-status mapper instead of a fixed `kind`. Its signature becomes `func syncStream(s *store.Store, url, token, cursorKey string, hc *http.Client, mapItem func(status) store.Item) (int, error)`, and in the item loop replace `items = append(items, toItem(st, kind))` with `items = append(items, mapItem(st))`. The stop/newest logic still keys on `st.ID` (top-level id) — unchanged.

Update the two existing callers inside `Sync` to pass mappers, and add the authorfeed stream:
```go
func Sync(s *store.Store, baseURL, token string, hc *http.Client) (int, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	like := func(st status) store.Item { return toItem(st, "like") }
	bookmark := func(st status) store.Item { return toItem(st, "bookmark") }
	author := func(st status) store.Item {
		if st.Reblog != nil {
			return toItem(*st.Reblog, "repost")
		}
		return toItem(st, "own")
	}

	fav, err := syncStream(s, baseURL+"/api/v1/favourites?limit=40", token, "mastodon:favourites", hc, like)
	if err != nil {
		return fav, err
	}
	bm, err := syncStream(s, baseURL+"/api/v1/bookmarks?limit=40", token, "mastodon:bookmarks", hc, bookmark)
	if err != nil {
		return fav + bm, err
	}
	id, err := verifyCredentials(baseURL, token, hc)
	if err != nil {
		return fav + bm, err
	}
	af, err := syncStream(s, baseURL+"/api/v1/accounts/"+id+"/statuses?exclude_replies=true&limit=40", token, "mastodon:authorfeed", hc, author)
	if err != nil {
		return fav + bm + af, err
	}
	return fav + bm + af, nil
}

func verifyCredentials(baseURL, token string, hc *http.Client) (string, error) {
	req, err := http.NewRequest("GET", baseURL+"/api/v1/accounts/verify_credentials", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("Mastodon auth failed (check MASTODON_ACCESS_TOKEN)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Mastodon API status %d for verify_credentials", resp.StatusCode)
	}
	var acc struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&acc); err != nil {
		return "", err
	}
	return acc.ID, nil
}
```
(Remove the now-unused `kind` parameter references; `toItem` is unchanged and still used by the mappers.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/mastodon/ -v`
Expected: PASS — existing favourites/bookmarks tests (with the added verify_credentials/statuses handlers) plus `TestSyncAuthorFeedOwnVsBoost`.

- [ ] **Step 5: Full regression + manual sync surface check**

Run:
```bash
mise exec -- go build ./... && mise exec -- go test ./...
mise exec -- go build -o /tmp/glane . && env -u GITHUB_TOKEN -u MASTODON_INSTANCE_URL -u MASTODON_ACCESS_TOKEN -u BLUESKY_HANDLE -u BLUESKY_APP_PASSWORD GLANE_DB=/tmp/glane-x.db /tmp/glane sync all; echo "exit=$?"
```
Expected: build clean, all packages PASS; `sync all` with nothing configured prints the skip line and `exit=0` (unchanged behavior — the new streams only add work inside the connectors).

- [ ] **Step 6: Commit**

```bash
git add internal/mastodon/
git commit -m "$(printf '%s' '🐘 Sync my Mastodon posts and boosts

Add an author-feed stream: verify_credentials resolves the account id, then
accounts/{id}/statuses is paged like favourites. A status with a non-null reblog
is a boost, mapped to the original reblogged status (its id/author/content/url)
with kind repost; the rest are my own posts. syncStream is generalized to a
per-status mapper so favourites/bookmarks/author-feed share one paging loop while
each assigns kind its own way.')"
```

---

## Follow-up (out of scope for this plan)

Capturing Bluesky `embed.external.uri` so liked/saved/own link posts enrich as well as Mastodon's do. A documented cron/launchd entry for scheduled `sync all` + `enrich` + `summarize`.
