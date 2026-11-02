package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func call(t *testing.T, in string) response {
	t.Helper()
	var res response
	if err := json.Unmarshal(serve([]byte(in)), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestServeWithoutDatabase(t *testing.T) {
	handler = nil
	if res := call(t, `{"method":"GET","url":"/"}`); res.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.Status)
	}
}

func TestServeRoundTrip(t *testing.T) {
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte(r.Method + " " + r.URL.RequestURI() + " " + r.Header.Get("Accept-Language")))
	})
	t.Cleanup(func() { handler = nil })
	res := call(t, `{"method":"GET","url":"/?q=go%20sqlite","header":{"Accept-Language":"fr"}}`)
	if res.Status != http.StatusTeapot || res.Header["Content-Type"] != "text/plain" {
		t.Fatalf("got %d %v", res.Status, res.Header)
	}
	if got, want := string(res.Body), "GET /?q=go%20sqlite fr"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
