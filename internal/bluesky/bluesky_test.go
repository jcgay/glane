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
