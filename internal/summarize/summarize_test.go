package summarize

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	res, err := c.Summarize(context.Background(), "Title", "body")
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
	res, err := c.Summarize(context.Background(), "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "S" || len(res.Tags) != 1 || res.Tags[0] != "go" {
		t.Fatalf("got %+v", res)
	}
}

func TestSummarizeErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("expected error")
	}
}
