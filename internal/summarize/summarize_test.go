package summarize

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func chatResponse(content string) string {
	b, _ := jsonMarshalString(content)
	return fmt.Sprintf(`{"choices":[{"message":{"content":%s}}]}`, b)
}

// jsonMarshalString quotes a string as a JSON string literal (test helper).
func jsonMarshalString(s string) (string, error) {
	out := `"`
	for _, r := range s {
		switch r {
		case '"':
			out += `\"`
		case '\\':
			out += `\\`
		case '\n':
			out += `\n`
		default:
			out += string(r)
		}
	}
	return out + `"`, nil
}

func TestSummarizeParsesPlainJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse(`{"summary":"It is about lambda cold starts.","tags":["AWS","lambda","lambda"]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	res, err := c.Summarize(context.Background(), "Title", "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "It is about lambda cold starts." {
		t.Fatalf("summary %q", res.Summary)
	}
	// lowercased + deduped
	if len(res.Tags) != 2 || res.Tags[0] != "aws" || res.Tags[1] != "lambda" {
		t.Fatalf("tags %v", res.Tags)
	}
}

func TestSummarizeExtractsFencedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse("Sure!\n```json\n{\"summary\":\"S\",\"tags\":[\"go\"]}\n```"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	res, err := c.Summarize(context.Background(), "t", "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "S" || len(res.Tags) != 1 || res.Tags[0] != "go" {
		t.Fatalf("got %+v", res)
	}
}

func TestSummarizePassesKnownTagsInPrompt(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		fmt.Fprint(w, chatResponse(`{"summary":"S","tags":["go"]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Summarize(context.Background(), "t", "b", []string{"golang", "aws"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "golang, aws") {
		t.Fatalf("known tags not in request: %s", gotBody)
	}
}

func TestSummarizeRetriesOn503(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, chatResponse(`{"summary":"S","tags":["go"]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()} // RetryBackoff 0 → instant
	res, err := c.Summarize(context.Background(), "t", "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "S" {
		t.Fatalf("got %+v", res)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 retries), got %d", calls)
	}
}

func TestSummarizeGivesUpAfterMaxRetries(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Summarize(context.Background(), "t", "b", nil); err == nil {
		t.Fatal("expected error")
	}
	if calls != maxRetries+1 {
		t.Fatalf("expected %d attempts, got %d", maxRetries+1, calls)
	}
}

func TestSummarizeErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Summarize(context.Background(), "t", "b", nil); err == nil {
		t.Fatal("expected error")
	}
}
