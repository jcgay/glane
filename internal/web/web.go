package web

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"time"

	gembed "github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

var funcs = template.FuncMap{
	// host strips a URL down to its display domain (no scheme, no "www.").
	"host": func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return raw
		}
		h := u.Host
		if len(h) > 4 && h[:4] == "www." {
			h = h[4:]
		}
		return h
	},
	// reltime renders a unix timestamp as a short, human relative age.
	"reltime": func(ts int64) string {
		if ts <= 0 {
			return ""
		}
		d := time.Since(time.Unix(ts, 0))
		switch {
		case d < time.Minute:
			return "à l'instant"
		case d < time.Hour:
			return fmt.Sprintf("il y a %dmin", int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf("il y a %dh", int(d.Hours()))
		case d < 30*24*time.Hour:
			return fmt.Sprintf("il y a %dj", int(d.Hours()/24))
		default:
			return time.Unix(ts, 0).Format("2 Jan 2006")
		}
	},
}

var tmpl = template.Must(template.New("").Funcs(funcs).ParseFS(assets, "templates/*.html"))

func handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		tags, err := s.TagCounts() // degrade silently: no tags just means no browse list
		if err != nil {
			log.Printf("glane: tag counts: %v", err)
		}
		if err := tmpl.ExecuteTemplate(w, "index.html", tags); err != nil {
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
