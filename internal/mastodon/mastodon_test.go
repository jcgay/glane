package mastodon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		case "/api/v1/accounts/verify_credentials":
			fmt.Fprint(w, `{"id":"1"}`)
		case "/api/v1/accounts/1/statuses":
			fmt.Fprint(w, `[]`)
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
		switch {
		case r.URL.Path == "/api/v1/favourites":
			fmt.Fprint(w, `[`+statusJSON(20)+`,`+statusJSON(19)+`,`+statusJSON(18)+`]`)
		case r.URL.Path == "/api/v1/accounts/verify_credentials":
			fmt.Fprint(w, `{"id":"1"}`)
		default:
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

// helpers for building fixture JSON
func statusJSON(id int) string {
	return fmt.Sprintf(`{"id":"%d","created_at":"2023-01-01T00:00:00Z","url":"https://m/@u/%d","content":"<p>text%d</p>","account":{"acct":"u@m"}}`, id, id, id)
}
func srvURL(r *http.Request) string { return "http://" + r.Host }

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
