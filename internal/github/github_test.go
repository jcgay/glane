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
