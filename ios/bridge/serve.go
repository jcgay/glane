// Command bridge is glane built as a C library for the iOS app
// (go build -buildmode=c-archive, see ios/build-go.sh). iOS lets no app
// start a process, so instead of running `glane serve` like Android, the app
// passes each WebView request to the read-only handler in its own process:
// no port, nothing another app could reach. The C exports are in bridge.go.
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

var (
	mu      sync.Mutex
	handler http.Handler // nil until GlaneOpen succeeds
)

// request and response cross the C boundary as JSON; bodies are base64,
// which is what Swift's Codable does with Data.
type request struct {
	Method string            `json:"method"`
	URL    string            `json:"url"` // path and query
	Header map[string]string `json:"header"`
	Body   []byte            `json:"body"`
}

type response struct {
	Status int               `json:"status"`
	Header map[string]string `json:"header"`
	Body   []byte            `json:"body"`
}

func serve(in []byte) []byte {
	var res response
	var req request
	mu.Lock()
	h := handler
	mu.Unlock()
	if err := json.Unmarshal(in, &req); err != nil {
		res = response{Status: http.StatusBadRequest, Body: []byte(err.Error())}
	} else if h == nil {
		res = response{Status: http.StatusServiceUnavailable, Body: []byte("no database open")}
	} else if r, err := http.NewRequest(req.Method, req.URL, bytes.NewReader(req.Body)); err != nil {
		res = response{Status: http.StatusBadRequest, Body: []byte(err.Error())}
	} else {
		for k, v := range req.Header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		res = response{Status: rec.Code, Header: map[string]string{}, Body: rec.Body.Bytes()}
		for k := range rec.Header() {
			res.Header[k] = rec.Header().Get(k)
		}
	}
	out, _ := json.Marshal(res)
	return out
}

func main() {}
