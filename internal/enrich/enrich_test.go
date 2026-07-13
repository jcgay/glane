package enrich

import (
	"strings"
	"testing"
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
