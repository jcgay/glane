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
	"github.com/jcgay/glane/internal/summarize"
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
	// head is the first n of xs (all of them when there are fewer), and more
	// how many it left out: results show three tags and a "+n".
	"head": func(n int, xs []string) []string { return xs[:min(n, len(xs))] },
	"more": func(n int, xs []string) int { return max(len(xs)-n, 0) },
	"inc":  func(i int) int { return i + 1 },
	"join": strings.Join,
	// pct is n as a whole percentage of total, for the stats meters.
	"pct": func(n, total int) int {
		if total <= 0 {
			return 0
		}
		return n * 100 / total
	},
	// reltime renders a unix timestamp as a short, human relative age. It takes
	// the catalog because template funcs can't see the view: call it as
	// {{reltime .CreatedAt $.T}}.
	"reltime": func(ts int64, t catalog) string {
		if ts <= 0 {
			return ""
		}
		d := time.Since(time.Unix(ts, 0))
		switch {
		case d < time.Minute:
			return t["justNow"]
		case d < time.Hour:
			return fmt.Sprintf(t["agoMin"], int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf(t["agoHour"], int(d.Hours()))
		case d < 30*24*time.Hour:
			return fmt.Sprintf(t["agoDay"], int(d.Hours()/24))
		default:
			return time.Unix(ts, 0).Format(t["dateFmt"])
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
	Query     query
}

// sources are the facets of the filter rail, in the order the 1–4 keys pick
// them. All four always show, so a key always lands on the same source.
var sources = []string{"twitter", "bluesky", "mastodon", "github"}

type facet struct {
	Source string
	Count  int
	Key    int
}

// home is what index.html renders: the filter rail, the index health and the
// status bar all read from it.
type home struct {
	Tags       []store.TagCount
	Stats      store.Stats
	Sources    []facet
	EmbedModel string // "" when semantic search is off
	Embed      bool
	Summary    bool // a summary endpoint is set, so `glane summarize` would do something
}

func handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		// degrade silently: no tags or stats just mean an emptier rail
		tags, err := s.TagCounts()
		if err != nil {
			log.Printf("glane: tag counts: %v", err)
		}
		st, err := s.Stats()
		if err != nil {
			log.Printf("glane: stats: %v", err)
		}
		h := home{Tags: tags, Stats: st}
		for i, src := range sources {
			f := facet{Source: src, Key: i + 1}
			for _, sc := range st.BySource {
				if sc.Source == src {
					f.Count = sc.Count
				}
			}
			h.Sources = append(h.Sources, f)
		}
		if c := gembed.FromEnv(); c != nil {
			h.Embed, h.EmbedModel = true, c.Model
		}
		h.Summary = summarize.FromEnv() != nil
		if err := tmpl.ExecuteTemplate(w, "index.html", view{pick(w, r), h}); err != nil {
			log.Printf("glane: render index.html: %v", err)
		}
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		st, err := s.Stats() // degrade silently: render zero-value stats rather than a hard 500
		if err != nil {
			log.Printf("glane: stats: %v", err)
		}
		if err := tmpl.ExecuteTemplate(w, "stats.html", view{pick(w, r), st}); err != nil {
			log.Printf("glane: render stats.html: %v", err)
		}
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query()
		qr := parseQuery(p.Get("q"), p.Get("source"), p.Get("tag"), p.Get("since"))
		q := qr.Words
		// A malformed date just means no date filter: "since:20" is what the box
		// holds halfway through typing "since:2026", and a review screen should
		// keep rendering rather than 500.
		since, err := store.ParseSince(qr.Since)
		if err != nil {
			qr.Since = ""
		}
		f := store.Filter{
			Source: qr.Source,
			Tag:    qr.Tag,
			Since:  since,
			Limit:  pageLimit,
		}
		// No query is the review listing (newest first), not an empty screen —
		// source, date and tag all narrow it.
		var res []store.Result
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
		pg := page{Hits: res, Truncated: len(res) == pageLimit, Query: qr}
		if err := tmpl.ExecuteTemplate(w, "results.html", view{pick(w, r), pg}); err != nil {
			log.Printf("glane: render results.html: %v", err)
		}
	})
	return mux
}

func Serve(s *store.Store, addr string) error {
	return http.ListenAndServe(addr, handler(s))
}
