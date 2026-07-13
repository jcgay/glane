package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedParsesOpenAIShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	vecs, err := c.Embed(context.Background(), []string{"hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 || vecs[0][1] != 0.2 {
		t.Fatalf("bad parse: %v", vecs)
	}
}

func TestEmbedReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), []string{"hi"})
	if err == nil {
		t.Fatal("expected error on non-200 response")
	}
}
