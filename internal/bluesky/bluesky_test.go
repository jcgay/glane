package bluesky

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

// post builds a feedViewPost fixture; rkey doubles as the distinctive text word.
func post(rkey string) string {
	return fmt.Sprintf(`{"post":{"uri":"at://did:plc:abc/app.bsky.feed.post/%s","cid":"c","author":{"handle":"alice.bsky.social","did":"did:plc:abc"},"record":{"text":"word%s","createdAt":"2023-05-01T00:00:00Z"},"indexedAt":"2023-05-01T00:00:00Z"}}`, rkey, rkey)
}

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

func TestSyncAuthThenLikesPaginate(t *testing.T) {
	srv := newServer(t, serverPages{likes: map[string]string{
		"":   fmt.Sprintf(`{"cursor":"c1","feed":[%s,%s]}`, post("aaa"), post("bbb")),
		"c1": fmt.Sprintf(`{"feed":[%s]}`, post("ccc")), // no cursor → last page
	}})
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
	srv := newServer(t, serverPages{likes: map[string]string{
		"": fmt.Sprintf(`{"cursor":"c1","feed":[%s,%s]}`, post("new"), post("old")),
	}})
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
	if cur, _ := s.GetCursor("bluesky:likes"); cur != "at://did:plc:abc/app.bsky.feed.post/new" {
		t.Fatalf("cursor should advance to newest liked URI, got %q", cur)
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
