package web

import (
	"bytes"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

// writeDB builds a glane database holding one item at path, through a temp
// file renamed over it: the same way Syncthing delivers a new copy.
func writeDB(t *testing.T, path, srcid, text string) {
	t.Helper()
	tmp := path + ".tmp"
	os.Remove(tmp)
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]store.Item{{Source: "bluesky", SourceID: srcid, Kind: "like", Text: text, URL: "http://x/" + srcid}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, r *reloader, q string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/search?q="+q, nil))
	if rec.Code != 200 {
		t.Fatalf("GET /search?q=%s: status %d: %s", q, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func TestReloaderPicksUpReplacedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glane.db")
	writeDB(t, p, "1", "alpha")
	r := &reloader{path: p}

	if !strings.Contains(get(t, r, "alpha"), "http://x/1") {
		t.Fatal("first copy not served")
	}
	writeDB(t, p, "2", "beta")
	if !strings.Contains(get(t, r, "beta"), "http://x/2") {
		t.Fatal("replaced copy not picked up")
	}
}

func TestReloaderKeepsLastGoodCopy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glane.db")
	writeDB(t, p, "1", "alpha")
	r := &reloader{path: p}
	get(t, r, "alpha")

	tmp := p + ".tmp"
	os.WriteFile(tmp, []byte("truncated download"), 0o644)
	os.Rename(tmp, p)
	if !strings.Contains(get(t, r, "alpha"), "http://x/1") {
		t.Fatal("a corrupt copy replaced the last good one")
	}
}

func TestReloaderSurvivesMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glane.db")
	writeDB(t, p, "1", "alpha")
	r := &reloader{path: p}
	get(t, r, "alpha")

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	os.Remove(p)
	if !strings.Contains(get(t, r, "alpha"), "http://x/1") {
		t.Fatal("a missing file dropped the last good copy")
	}
	get(t, r, "alpha")
	if n := strings.Count(logs.String(), "\n"); n != 1 {
		t.Errorf("logged %d lines while the file was missing, want 1:\n%s", n, logs.String())
	}
	writeDB(t, p, "2", "beta")
	if !strings.Contains(get(t, r, "beta"), "http://x/2") {
		t.Fatal("the copy arriving after the gap was not picked up")
	}
}

func TestServeReadOnlyFailsFastOnMissingFile(t *testing.T) {
	err := ServeReadOnly(filepath.Join(t.TempDir(), "missing.db"), "127.0.0.1:0")
	if err == nil {
		t.Fatal("ServeReadOnly started without a database")
	}
}
