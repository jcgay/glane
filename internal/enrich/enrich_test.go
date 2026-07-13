package enrich

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestFirstURL(t *testing.T) {
	if got := FirstURL("hi https://t.co/abc and more"); got != "https://t.co/abc" {
		t.Fatalf("got %q", got)
	}
	if got := FirstURL("no link here"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestExtractPullsBody(t *testing.T) {
	html := `<html><head><title>My Post</title></head><body><article>
		<p>The cold start problem in AWS Lambda is about init latency.</p>
		<p>Provisioned concurrency helps.</p></article></body></html>`
	title, text, err := Extract(strings.NewReader(html), "http://example.com/post")
	if err != nil {
		t.Fatal(err)
	}
	if title != "My Post" {
		t.Fatalf("title %q", title)
	}
	if !strings.Contains(text, "cold start") {
		t.Fatalf("body not extracted: %q", text)
	}
}

// TestRunIsolatesFailures pins down the Run loop's binding constraint: a
// per-item fetch/extract failure must never abort the run, and the failed
// item must remain searchable by its own post text. A regression like an
// early return instead of continue on error would break this silently
// while still passing a naive "no crash" smoke test.
func TestRunIsolatesFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good" {
			w.Write([]byte(`<html><head><title>Good</title></head><body><article><p>provisioned concurrency for aws lambda cold starts</p></article></body></html>`))
			return
		}
		http.Error(w, "nope", 500)
	}))
	defer srv.Close()

	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	items := []store.Item{
		{Source: "twitter", SourceID: "1", Text: "post a", URL: srv.URL + "/good"},
		{Source: "twitter", SourceID: "2", Text: "post b about kubernetes networking", URL: srv.URL + "/bad"},
	}
	if _, err := s.Upsert(items); err != nil {
		t.Fatal(err)
	}

	done, failed, err := Run(s, srv.Client(), 10)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if done != 1 {
		t.Fatalf("done = %d, want 1", done)
	}
	if failed != 1 {
		t.Fatalf("failed = %d, want 1", failed)
	}

	okHits, err := s.SearchFTS("provisioned", store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(okHits) < 1 {
		t.Fatalf("expected item A's extracted article to be searchable, got %d hits", len(okHits))
	}

	failedHits, err := s.SearchFTS("kubernetes", store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(failedHits) < 1 {
		t.Fatalf("expected failed item B to still be searchable by its own text, got %d hits", len(failedHits))
	}
}
