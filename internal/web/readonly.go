package web

import (
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jcgay/glane/internal/store"
)

// ServeReadOnly serves the database at path without ever writing to it, and
// picks up a new copy as soon as one replaces the file (a sync tool renaming
// its download over it), without a restart.
func ServeReadOnly(path, addr string) error {
	r := &reloader{path: path}
	if _, err := r.current(); err != nil {
		return err
	}
	return http.ListenAndServe(addr, r)
}

// reloader is a handler over the newest readable copy of one database file.
type reloader struct {
	path string

	mu      sync.Mutex
	fi      os.FileInfo // the copy last tried, good or not
	missing bool        // no file at path on the last request
	s       *store.Store
	h       http.Handler
}

func (r *reloader) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h, err := r.current()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, req)
}

// current reopens the file when a different copy sits at path. A missing file
// or a copy that won't open keeps the last good one in service.
func (r *reloader) current() (http.Handler, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fi, err := os.Stat(r.path)
	wasMissing := r.missing
	r.missing = err != nil
	if err == nil && r.fi != nil && os.SameFile(fi, r.fi) && fi.ModTime().Equal(r.fi.ModTime()) {
		return r.h, nil
	}
	var s *store.Store
	if err == nil {
		s, err = store.OpenReadOnly(r.path)
	}
	if err != nil {
		if r.h == nil {
			return nil, err // first open: ServeReadOnly reports it and exits
		}
		if fi != nil {
			r.fi = fi // try a bad copy once, not on every request
		}
		if fi != nil || !wasMissing { // a missing file: say it once, not on every request
			log.Printf("glane: keeping the previous database: %v", err)
		}
		return r.h, nil
	}
	if old := r.s; old != nil {
		// ponytail: requests still running on the old copy get a minute to
		// finish before it closes; a slower one would fail mid-query
		time.AfterFunc(time.Minute, func() { old.Close() })
	}
	r.fi, r.s, r.h = fi, s, handler(s)
	return r.h, nil
}
