package web

import (
	"net/http"
	"strings"
)

// Two flat catalogs, no message framework: the UI is three templates. Keys are
// the English-ish label, values the rendered text. A missing key renders empty,
// so TestCatalogsMatch guards the pair against typos and drift.
type catalog map[string]string

var en = catalog{
	"lang":              "en",
	"title":             "glane · tech watch",
	"description":       "Search your tech watch: repos, articles and posts gleaned along the way.",
	"tagline":           "your watch, found in a word",
	"stats":             "Stats",
	"searchPlaceholder": "Search a repo, an article, a post…",
	"searchAria":        "Search your tech watch",
	"sourceAria":        "Filter by source",
	"allSources":        "all",
	"since":             "since",
	"sinceAria":         "Only show items from this date on",
	"tags":              "tags",
	"clearTagAria":      "Clear the tag filter",

	"result":    "result",
	"truncated": "— newest only, narrow the date or source to see further",
	"ftsTitle":  "Found by full-text search",
	"fts":       "text",
	"semTitle":  "Found by semantic search",
	"sem":       "semantic",
	"on":        "on",
	"noResults": "No results. Try other keywords, switch source or widen the date.",

	"statsTitle":       "glane · stats",
	"statsDescription": "Statistics on your gleaned tech watch: items imported, enriched, summarized and vectorized.",
	"backToSearch":     "← Search",
	"total":            "Total",
	"enriched":         "Enriched",
	"summarized":       "Summaries",
	"embeddings":       "Embeddings",
	"tagsLabel":        "Tags",
	"bySource":         "By source",
	"lastSync":         "Last sync",

	"justNow": "just now",
	"agoMin":  "%dmin ago",
	"agoHour": "%dh ago",
	"agoDay":  "%dd ago",
}

var fr = catalog{
	"lang":              "fr",
	"title":             "glane · veille techno",
	"description":       "Recherche dans votre veille technologique : dépôts, articles et posts glanés au fil de l'eau.",
	"tagline":           "votre veille, retrouvée d'un mot",
	"stats":             "Stats",
	"searchPlaceholder": "Rechercher un dépôt, un article, un post…",
	"searchAria":        "Rechercher dans la veille",
	"sourceAria":        "Filtrer par source",
	"allSources":        "toutes",
	"since":             "depuis",
	"sinceAria":         "N'afficher que les items à partir de cette date",
	"tags":              "tags",
	"clearTagAria":      "Retirer le filtre par tag",

	"result":    "résultat",
	"truncated": "— les plus récents seulement, affinez la date ou la source pour voir au-delà",
	"ftsTitle":  "Trouvé par la recherche plein texte",
	"fts":       "texte",
	"semTitle":  "Trouvé par la recherche sémantique",
	"sem":       "sémantique",
	"on":        "sur",
	"noResults": "Aucun résultat. Essayez d'autres mots-clés, changez de source ou élargissez la date.",

	"statsTitle":       "glane · statistiques",
	"statsDescription": "Statistiques sur votre veille technologique glanée : items importés, enrichis, résumés et vectorisés.",
	"backToSearch":     "← Recherche",
	"total":            "Total",
	"enriched":         "Enrichis",
	"summarized":       "Résumés",
	"embeddings":       "Embeddings",
	"tagsLabel":        "Tags",
	"bySource":         "Par source",
	"lastSync":         "Dernier sync",

	"justNow": "à l'instant",
	"agoMin":  "il y a %dmin",
	"agoHour": "il y a %dh",
	"agoDay":  "il y a %dj",
}

// pick reads the browser's Accept-Language. Header-only means the htmx
// fragments follow the page with no cookie, no ?lang, no state to carry.
//
// ponytail: takes the first fr/en tag in header order rather than sorting by
// q-value — browsers already send them in preference order. Pull in
// golang.org/x/text/language if a hand-written header ever needs to win.
func pick(r *http.Request) catalog {
	for _, tag := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		switch {
		case strings.HasPrefix(strings.TrimSpace(tag), "fr"):
			return fr
		case strings.HasPrefix(strings.TrimSpace(tag), "en"):
			return en
		}
	}
	return en
}

// view is what every template receives: T for the labels, D for the data the
// handler already had.
type view struct {
	T catalog
	D any
}
