package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/summarize"
)

func TestSplitQueryArgs(t *testing.T) {
	cases := []struct {
		args      []string
		wantQuery string
		wantFlags []string
	}{
		{[]string{"lambda"}, "lambda", []string{}},
		{[]string{"lambda", "calculus", "--limit", "2"}, "lambda calculus", []string{"--limit", "2"}},
		{[]string{"cold start", "--source", "github"}, "cold start", []string{"--source", "github"}},
		{[]string{"--limit", "2"}, "", []string{"--limit", "2"}},
	}
	for _, c := range cases {
		gotQuery, gotFlags := splitQueryArgs(c.args)
		if gotQuery != c.wantQuery || !reflect.DeepEqual(gotFlags, c.wantFlags) {
			t.Errorf("splitQueryArgs(%v) = (%q, %v), want (%q, %v)",
				c.args, gotQuery, gotFlags, c.wantQuery, c.wantFlags)
		}
	}
}

func TestParseSince(t *testing.T) {
	if ts, err := parseSince(""); ts != 0 || err != nil {
		t.Errorf("parseSince(\"\") = (%d, %v), want (0, nil)", ts, err)
	}
	if ts, err := parseSince("2023"); ts <= 0 || err != nil {
		t.Errorf("parseSince(\"2023\") = (%d, %v), want (positive, nil)", ts, err)
	}
	if ts, err := parseSince("2023-06-15"); ts <= 0 || err != nil {
		t.Errorf("parseSince(\"2023-06-15\") = (%d, %v), want (positive, nil)", ts, err)
	}
	if _, err := parseSince("nope"); err == nil {
		t.Errorf("parseSince(\"nope\") = nil error, want an error")
	}
}

func TestSyncAllSkipsWhenUnconfigured(t *testing.T) {
	// All connector env empty → every connector skipped → no failure.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("MASTODON_INSTANCE_URL", "")
	t.Setenv("MASTODON_ACCESS_TOKEN", "")
	t.Setenv("BLUESKY_HANDLE", "")
	t.Setenv("BLUESKY_APP_PASSWORD", "")
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if syncAll(s) {
		t.Fatal("syncAll should report no failure when everything is skipped")
	}
}

// updateServer serves article HTML for any path except /chat/completions,
// which returns an OpenAI-shaped chat response with a JSON summary+tags.
func updateServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"a summary about lambdas\",\"tags\":[\"aws\"]}"}}]}`))
			return
		}
		w.Write([]byte(`<html><head><title>T</title></head><body><article><p>content about aws lambda</p></article></body></html>`))
	}))
}

func TestEnrichAllDrainsBacklog(t *testing.T) {
	srv := updateServer(t)
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.Upsert([]store.Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "a", URL: srv.URL + "/a"},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "b", URL: srv.URL + "/b"},
	})
	done, _, errored := enrichAll(s, srv.Client(), nil)
	if errored || done != 2 {
		t.Fatalf("want 2 enriched, no error; got done=%d errored=%v", done, errored)
	}
	if pend, _ := s.PendingEnrichment(10); len(pend) != 0 {
		t.Fatalf("backlog not drained: %d still pending", len(pend))
	}
}

func TestSummarizeAllDrainsThenFailsSafe(t *testing.T) {
	srv := updateServer(t)
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "a", URL: srv.URL + "/a"}})
	// enrich first so there's an article to summarize
	enrichAll(s, srv.Client(), nil)

	c := &summarize.Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	done, failed := summarizeAll(s, c)
	if done != 1 || failed != 0 {
		t.Fatalf("want 1 summarized; got done=%d failed=%d", done, failed)
	}
	if pend, _ := s.PendingSummary(10); len(pend) != 0 {
		t.Fatalf("summary backlog not drained: %d pending", len(pend))
	}

	// Failure path: a bad endpoint must NOT loop forever — single pass returns.
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 500)
	}))
	defer srvBad.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "2", Kind: "like", Text: "b", URL: srv.URL + "/b"}})
	enrichAll(s, srv.Client(), nil)
	cBad := &summarize.Client{BaseURL: srvBad.URL, Model: "m", HTTP: srvBad.Client()}
	d2, f2 := summarizeAll(s, cBad)
	if d2 != 0 || f2 == 0 {
		t.Fatalf("failure path: want 0 done, >0 failed; got done=%d failed=%d", d2, f2)
	}
}

func TestRenderResultsFlattensAndNumbers(t *testing.T) {
	res := []store.Result{
		{Item: store.Item{Source: "bluesky", Kind: "bookmark", Text: "Nouvelle vidéo\n\nsur plusieurs\nlignes", URL: "https://bsky.app/x", Tags: []string{"ia"}}},
		{Item: store.Item{Source: "github", Kind: "star", ArticleSummary: "A tool", URL: "https://gh/y"}},
	}
	got := renderResults(res)
	if strings.Contains(got, "vidéo\n\nsur") {
		t.Fatalf("snippet newlines not flattened:\n%s", got)
	}
	for _, want := range []string{"  1. ", "  2. ", "Nouvelle vidéo sur plusieurs lignes", "#ia", "(2 results)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderResultsShowsExcerpt(t *testing.T) {
	res := []store.Result{
		{Item: store.Item{Source: "github", Kind: "star", Text: "cool physics", URL: "https://gh/1"},
			Snippet: "a note on quantum " + store.MarkStart + "entanglement" + store.MarkEnd + " today"},
	}
	got := renderResults(res)
	// Excerpt text appears; test stdout is not a TTY, so sentinels are stripped
	// (no raw control bytes leak into piped output).
	if !strings.Contains(got, "quantum entanglement today") {
		t.Fatalf("excerpt missing from output:\n%q", got)
	}
	if strings.ContainsAny(got, store.MarkStart+store.MarkEnd) {
		t.Fatalf("match sentinels leaked into piped output:\n%q", got)
	}
}

func TestCutRunes(t *testing.T) {
	got := cutRunes("café☕more", 5)
	if !utf8.ValidString(got) {
		t.Fatalf("cutRunes produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 5 {
		t.Fatalf("cutRunes rune count = %d, want 5", n)
	}
	if got := cutRunes("hi", 5); got != "hi" {
		t.Fatalf("cutRunes short string: got %q, want %q", got, "hi")
	}
}
