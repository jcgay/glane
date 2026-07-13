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
