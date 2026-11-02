package web

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	gembed "github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/summarize"
)

//go:embed templates/*.html static/*
var assets embed.FS

// AppIcon is the 512 px PNG the manifest uses, for a native window's Dock
// or taskbar icon.
func AppIcon() []byte {
	b, _ := assets.ReadFile("static/icon-512.png") // embedded: can't fail
	return b
}

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
	// front moves x to the start of xs when present: the tag being browsed
	// leads a result's tags instead of hiding behind "+n".
	"front": func(x string, xs []string) []string {
		i := slices.Index(xs, x)
		if i <= 0 {
			return xs
		}
		return slices.Concat([]string{x}, xs[:i], xs[i+1:])
	},
	"inc": func(i int) int { return i + 1 },
	// num groups thousands the way the page's language writes them: 8,306
	// in English, 8 306 (narrow no-break space) in French.
	"num": func(n int, t catalog) string {
		s := strconv.Itoa(n)
		for i := len(s) - 3; i > 0 && n > 0; i -= 3 {
			s = s[:i] + t["thousands"] + s[i:]
		}
		return s
	},
	// results is "result" or "results" for n: French keeps 0 singular
	"results": func(n int, t catalog) string {
		if n == 1 || n == 0 && t["lang"] == "fr" {
			return t["result"]
		}
		return t["results"]
	},
	// op spells a filter the way the search box reads it, for a link to /?q=
	"op": func(name, v string) string {
		if strings.ContainsAny(v, " \t") {
			v = `"` + v + `"`
		}
		return name + ":" + v
	},
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

// pageLimit is one page of /search. A listing that hit it says so and offers
// the next page: one that silently stops reads as "that is everything that
// piled up", which is a wrong answer, not a short one. "More" asks for the
// same search with ?n= one page longer, up to maxLimit.
// ponytail: each "more" re-renders the whole list; switch to an offset and
// append only if lists past a few hundred rows feel slow.
const (
	pageLimit = 50
	maxLimit  = 1000
)

// page is what results.html renders.
type page struct {
	Hits      []store.Result
	Truncated bool
	Next      int // ?n= for the next page; 0 when there is none to offer
	NextN     int // how many that next page adds
	Of        int // how many match in all, when more than Hits and known exactly
	Query     query
	Rail      rail
	Notes     []string // filters typed but not applied, for the status line
}

// sources are the facets of the filter rail, in the order the 1–4 keys pick
// them. All four always show, so a key always lands on the same source.
var sources = []string{"twitter", "bluesky", "mastodon", "github"}

type facet struct {
	Source string
	Count  int
	Key    int
	Active bool
}

// The rail shows topTags tags and folds the rest. A tag on more than
// broadShare of the listing narrows it too little to lead: it ranks after
// the distinctive ones.
const (
	topTags    = 20
	broadShare = 0.3
)

// rail is what the source and tag facets render (facets.html).
type rail struct {
	Total    int
	Sources  []facet
	Tags     []store.TagCount // the first topTags, the browsed one included
	MoreTags []store.TagCount // the rest, folded
	Years    []store.YearCount
	Query    query
	OOB      bool // sent along a /search fragment, swapped in by id
}

func newRail(fc store.Facets, q query) rail {
	r := rail{Total: fc.Total, Tags: fc.Tags, Years: fc.Years, Query: q}
	for i, src := range sources {
		f := facet{Source: src, Key: i + 1, Active: src == q.Source}
		for _, sc := range fc.BySource {
			if sc.Source == src {
				f.Count = sc.Count
			}
		}
		r.Sources = append(r.Sources, f)
	}
	// the tag being browsed stays listed, so it can be cleared from the rail
	// even when nothing matches it any more
	if q.Tag != "" && !slices.ContainsFunc(r.Tags, func(t store.TagCount) bool { return t.Tag == q.Tag }) {
		r.Tags = append(r.Tags, store.TagCount{Tag: q.Tag})
	}
	broad := func(t store.TagCount) bool { return float64(t.Count) > broadShare*float64(fc.TagBase) }
	slices.SortStableFunc(r.Tags, func(a, b store.TagCount) int {
		switch {
		case broad(a) == broad(b):
			return 0
		case broad(a):
			return 1
		}
		return -1
	})
	r.Tags, r.MoreTags = r.Tags[:min(topTags, len(r.Tags))], r.Tags[min(topTags, len(r.Tags)):]
	if i := slices.IndexFunc(r.MoreTags, func(t store.TagCount) bool { return t.Tag == q.Tag }); i >= 0 {
		r.Tags = slices.Concat([]store.TagCount{r.MoreTags[i]}, r.Tags)
		r.MoreTags = slices.Delete(r.MoreTags, i, i+1)
	}
	if y := q.Year(); y != "" && !slices.ContainsFunc(r.Years, func(c store.YearCount) bool { return c.Year == y }) {
		r.Years = append(r.Years, store.YearCount{Year: y})
		slices.SortFunc(r.Years, func(a, b store.YearCount) int { return strings.Compare(b.Year, a.Year) })
	}
	return r
}

// statsPage is what stats.html renders: the counts, then the ways into the
// index (sources, years, tags), each a link to that listing.
type statsPage struct {
	store.Stats
	Years   []store.YearCount
	YearMax int // the busiest year: years are drawn against it, to show their shape
	Tags    []store.TagCount
	Embed   bool // an embedding endpoint is set: `glane enrich` would fill embeddings
	Summary bool // a summary endpoint is set: `glane summarize` would do something
}

// home is what index.html renders: the filter rail, the index health and the
// status bar all read from it.
type home struct {
	Rail       rail
	Stats      store.Stats
	EmbedModel string // "" when semantic search is off
	Embed      bool
	Summary    bool   // a summary endpoint is set, so `glane summarize` would do something
	Q          string // ?q=, so a search survives a reload and can be shared
	Sync       syncLine
}

// staleAfter is how long a source can go without a sync before the status
// bar points at it.
const staleAfter = 30 * 24 * time.Hour

// syncLine is the status bar's one line on syncing: when the last one ran,
// and which sources have fallen behind. /stats lists every cursor.
type syncLine struct {
	Latest int64
	Stale  []store.SourceSync // per source, its freshest cursor
}

// newSyncLine groups the cursors by source ("mastodon:bookmarks" is
// mastodon): a cursor only moves when its feed has something new, so a
// source is behind only when none of its cursors moved lately.
func newSyncLine(cursors []store.SourceSync, now time.Time) syncLine {
	var l syncLine
	fresh := map[string]int64{}
	var order []string
	for _, c := range cursors {
		src, _, _ := strings.Cut(c.Source, ":")
		if _, ok := fresh[src]; !ok {
			order = append(order, src)
		}
		fresh[src] = max(fresh[src], c.UpdatedAt)
		l.Latest = max(l.Latest, c.UpdatedAt)
	}
	for _, src := range order {
		if now.Sub(time.Unix(fresh[src], 0)) > staleAfter {
			l.Stale = append(l.Stale, store.SourceSync{Source: src, UpdatedAt: fresh[src]})
		}
	}
	return l
}

// Handler serves the whole web UI over s. Serve, ServeReadOnly and
// `glane app` all mount it.
func Handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		// degrade silently: no facets or stats just mean an emptier rail
		fc, err := s.Facets("", store.Filter{})
		if err != nil {
			log.Printf("glane: facets: %v", err)
		}
		st, err := s.Stats()
		if err != nil {
			log.Printf("glane: stats: %v", err)
		}
		h := home{Rail: newRail(fc, query{}), Stats: st, Q: r.URL.Query().Get("q"), Sync: newSyncLine(st.LastSyncBySource, time.Now())}
		if c := gembed.FromEnv(); c != nil {
			h.Embed, h.EmbedModel = true, c.Model
		}
		h.Summary = summarize.FromEnv() != nil
		if err := tmpl.ExecuteTemplate(w, "index.html", view{pick(w, r), h}); err != nil {
			log.Printf("glane: render index.html: %v", err)
		}
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		// degrade silently: render zero-value stats rather than a hard 500
		st, err := s.Stats()
		if err != nil {
			log.Printf("glane: stats: %v", err)
		}
		fc, err := s.Facets("", store.Filter{})
		if err != nil {
			log.Printf("glane: facets: %v", err)
		}
		// the same telling tags the rail leads with, and every year
		rl := newRail(fc, query{})
		pg := statsPage{Stats: st, Years: rl.Years, Tags: rl.Tags,
			Embed: gembed.FromEnv() != nil, Summary: summarize.FromEnv() != nil}
		for _, y := range pg.Years {
			pg.YearMax = max(pg.YearMax, y.Count)
		}
		if err := tmpl.ExecuteTemplate(w, "stats.html", view{pick(w, r), pg}); err != nil {
			log.Printf("glane: render stats.html: %v", err)
		}
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		t := pick(w, r)
		p := r.URL.Query()
		qr, kept := parseQuery(p.Get("q"), p.Get("source"), p.Get("tag"), p.Get("since"), p.Get("until"))
		q := qr.Words
		var notes []string
		for _, k := range kept {
			notes = append(notes, fmt.Sprintf(t["noteRepeated"], k))
		}
		// A malformed date just means no date filter: "since:20" is what the box
		// holds halfway through typing "since:2026", and a review screen should
		// keep rendering rather than 500. Same for a source still being typed.
		since, err := store.ParseSince(qr.Since)
		if err != nil {
			notes = append(notes, fmt.Sprintf(t["noteSince"], qr.Since))
			qr.Since = ""
		}
		until, err := store.ParseUntil(qr.Until)
		if err != nil {
			notes = append(notes, fmt.Sprintf(t["noteUntil"], qr.Until))
			qr.Until = ""
		}
		limit, _ := strconv.Atoi(p.Get("n"))
		limit = min(max(limit, pageLimit), maxLimit)
		if qr.Source != "" && !slices.Contains(sources, qr.Source) {
			notes = append(notes, fmt.Sprintf(t["noteSource"], qr.Source))
			qr.Source = ""
		}
		f := store.Filter{
			Source: qr.Source,
			Tag:    qr.Tag,
			Since:  since,
			Until:  until,
			Limit:  limit,
		}
		// No query is the review listing (newest first), not an empty screen —
		// source, date and tag all narrow it.
		ec := gembed.FromEnv()
		var res []store.Result
		if q != "" {
			res, err = search.Hybrid(s, ec, q, f)
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
		// counted on the same filters, minus the result limit; a failure only
		// leaves the rail showing zeros
		fc, err := s.Facets(q, store.Filter{Source: f.Source, Tag: f.Tag, Since: f.Since, Until: f.Until})
		if err != nil {
			log.Printf("glane: facets: %v", err)
		}
		rl := newRail(fc, qr)
		rl.OOB = true
		pg := page{Hits: res, Truncated: len(res) == limit, Query: qr, Rail: rl, Notes: notes}
		// the rail counts the same listing, but full-text only: exact unless
		// semantic search can add items it misses
		matched := -1
		if q == "" || ec == nil {
			matched = rl.Total
			for _, sf := range rl.Sources {
				if sf.Active {
					matched = sf.Count
				}
			}
			pg.Truncated = matched > len(res)
			if pg.Truncated {
				pg.Of = matched
			}
		}
		if pg.Truncated && limit < maxLimit {
			pg.Next, pg.NextN = limit+pageLimit, pageLimit
			if matched >= 0 {
				pg.NextN = min(pageLimit, matched-len(res))
			}
		}
		if err := tmpl.ExecuteTemplate(w, "results.html", view{t, pg}); err != nil {
			log.Printf("glane: render results.html: %v", err)
		}
	})
	return mux
}

func Serve(s *store.Store, addr string) error {
	return http.ListenAndServe(addr, Handler(s))
}
