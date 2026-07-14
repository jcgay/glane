package web

import (
	"embed"
	"html/template"
	"log"
	"net/http"

	gembed "github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

var tmpl = template.Must(template.ParseFS(assets, "templates/*.html"))

func handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "index.html", nil); err != nil {
			log.Printf("glane: render index.html: %v", err)
		}
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		tag := r.URL.Query().Get("tag")
		if q == "" && tag == "" {
			w.Write([]byte(""))
			return
		}
		var res []store.Result
		var err error
		if q == "" {
			res, err = s.ByTag(tag, store.Filter{Limit: 50})
		} else {
			res, err = search.Hybrid(s, gembed.FromEnv(), q, store.Filter{
				Source: r.URL.Query().Get("source"),
				Tag:    tag,
				Limit:  50,
			})
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := s.AttachTags(res); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "results.html", res); err != nil {
			log.Printf("glane: render results.html: %v", err)
		}
	})
	return mux
}

func Serve(s *store.Store, addr string) error {
	return http.ListenAndServe(addr, handler(s))
}
