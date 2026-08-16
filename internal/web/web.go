package web

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	gembed "github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

var markReplacer = strings.NewReplacer(store.MarkStart, "<mark>", store.MarkEnd, "</mark>")

var funcs = template.FuncMap{
	// mark renders an FTS snippet as safe HTML: escape the article text first
	// (it can contain arbitrary HTML), then turn the neutral match sentinels
	// into <mark> tags. Escaping before replacing is what keeps this XSS-safe.
	"mark": func(snip string) template.HTML {
		return template.HTML(markReplacer.Replace(template.HTMLEscapeString(snip)))
	},
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

// pageLimit caps one /search response. There is no pagination, so the fragment
// says when it hit the cap: a review listing that silently stops reads as
// "that is everything that piled up", which is a wrong answer, not a short one.
const pageLimit = 50

// page is what results.html renders.
type page struct {
	Hits      []store.Result
	Truncated bool
}

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

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.Stats() // degrade silently: render zero-value stats rather than a hard 500
		if err != nil {
			log.Printf("glane: stats: %v", err)
		}
		if err := tmpl.ExecuteTemplate(w, "stats.html", st); err != nil {
			log.Printf("glane: render stats.html: %v", err)
		}
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		// A malformed date just means no date filter — hand-crafted URLs aside,
		// the picker itself reports "" until the date is complete, and a review
		// screen should keep rendering rather than 500.
		since, _ := store.ParseSince(r.URL.Query().Get("since"))
		f := store.Filter{
			Source: r.URL.Query().Get("source"),
			Tag:    r.URL.Query().Get("tag"),
			Since:  since,
			Limit:  pageLimit,
		}
		// No query is the review listing (newest first), not an empty screen —
		// source, date and tag all narrow it.
		var res []store.Result
		var err error
		if q != "" {
			res, err = search.Hybrid(s, gembed.FromEnv(), q, f)
		} else {
			res, err = s.Recent(f)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := s.AttachTags(res); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		p := page{Hits: res, Truncated: len(res) == pageLimit}
		if err := tmpl.ExecuteTemplate(w, "results.html", p); err != nil {
			log.Printf("glane: render results.html: %v", err)
		}
	})
	return mux
}

func Serve(s *store.Store, addr string) error {
	return http.ListenAndServe(addr, handler(s))
}
