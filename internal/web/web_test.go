package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestSearchFragmentRendersHits(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "aws lambda cold start", URL: "http://x"}})

	req := httptest.NewRequest("GET", "/search?q=lambda", nil)
	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "lambda") {
		t.Fatalf("fragment missing hit: %s", rec.Body.String())
	}
}
