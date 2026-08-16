package web

import (
	"html/template"
	"io/fs"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jcgay/glane/internal/store"
)

// A key present in one catalog and not the other renders as an empty label,
// silently — the templates have no way to complain. This is the complaint.
func TestCatalogsMatch(t *testing.T) {
	for k := range en {
		if _, ok := fr[k]; !ok {
			t.Errorf("key %q missing from fr", k)
		}
	}
	for k := range fr {
		if _, ok := en[k]; !ok {
			t.Errorf("key %q missing from en", k)
		}
	}
}

// Key parity is not enough: {{.T.tagilne}} renders empty just as silently, and
// a typo added to both catalogs passes TestCatalogsMatch. So read the templates
// back and check every key they ask for actually exists.
func TestTemplateKeysExist(t *testing.T) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("no templates found: %v", err)
	}
	ref := regexp.MustCompile(`\.T\.(\w+)`)
	for _, f := range files {
		b, err := fs.ReadFile(assets, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s references unknown key %q", f, m[1])
			}
		}
	}
}

// Every page and fragment must follow Accept-Language, English by default.
func TestPagesFollowAcceptLanguage(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	// dated a year back so reltime falls past its 30-day window and renders the
	// date itself — the layout is the one label that lives outside the templates
	old := time.Now().AddDate(-1, 0, 0)
	s.Upsert([]store.Item{{Source: "github", SourceID: "1", Kind: "star", Text: "x", URL: "http://x/1", CreatedAt: old.Unix()}})

	for _, tc := range []struct {
		header, path, want string
	}{
		{"", "/", en["tagline"]},
		{"fr-FR,fr;q=0.9,en;q=0.8", "/", fr["tagline"]},
		{"en-US,en;q=0.9", "/", en["tagline"]},
		{"de-DE,de;q=0.9,fr;q=0.7", "/", fr["tagline"]}, // first supported wins
		{"EN-US", "/", en["tagline"]},                   // tags are case-insensitive
		{"frr", "/", en["tagline"]},                     // Northern Frisian is not French
		{"fr;q=0, en", "/", en["tagline"]},              // q=0 means "not acceptable"
		{"de, fr ;q=0.9", "/", fr["tagline"]},           // space before the weight is legal
		{"fr", "/stats", fr["bySource"]},
		{"", "/stats", en["bySource"]},
		{"fr", "/search?q=nope", fr["noResults"]},
		{"", "/search?q=nope", en["noResults"]},
		// dates are labels too: Go's month names are English only
		{"fr", "/search", old.Format("02/01/2006")},
		{"", "/search", old.Format("2 Jan 2006")},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		if tc.header != "" {
			req.Header.Set("Accept-Language", tc.header)
		}
		rec := httptest.NewRecorder()
		handler(s).ServeHTTP(rec, req)
		// the labels go through html/template, so compare what it emits
		if !strings.Contains(rec.Body.String(), template.HTMLEscapeString(tc.want)) {
			t.Errorf("Accept-Language %q on %s: want %q in body, got: %s", tc.header, tc.path, tc.want, rec.Body.String())
		}
		// negotiated on a header, so it must not be cached across languages
		if got := rec.Header().Get("Vary"); got != "Accept-Language" {
			t.Errorf("%s: Vary = %q, want Accept-Language", tc.path, got)
		}
	}
}
