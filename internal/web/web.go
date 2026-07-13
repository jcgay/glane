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
		if q == "" {
			w.Write([]byte(""))
			return
		}
		res, err := search.Hybrid(s, gembed.FromEnv(), q, store.Filter{Source: r.URL.Query().Get("source"), Limit: 50})
		if err != nil {
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
