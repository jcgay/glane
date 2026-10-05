package web

import (
	"net/http"
	"strconv"
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
	"searchPlaceholder": "words   source:github   tag:go   since:30d",
	"searchAria":        "Search your tech watch",
	"sourceAria":        "Filter by source",
	"allSources":        "all",
	"since":             "since",
	"sinceAria":         "Only show items from this far back",
	"tags":              "tags",
	"search":            "search",
	"pagesAria":         "Pages",
	"skipToResults":     "Skip to results",
	"filters":           "filters",
	"filtersAria":       "Filters",
	"toggleFilters":     "Show or hide the filters",
	"allTime":           "all",
	"copyFix":           "Copy the command that fills this gap",
	"items":             "items",
	"off":               "off",
	"keyMove":           "move",
	"keyOpen":           "open",
	"keyCopy":           "copy url",
	"keyHelp":           "help",
	"helpKeys":          "keyboard",
	"helpFocus":         "go to the search box; esc leaves it and keeps the query",
	"helpMove":          "next / previous result",
	"helpOpen":          "open the link; O opens the original post",
	"helpCopy":          "copy the url; Y copies a markdown link",
	"helpSources":       "filter twitter, bluesky, mastodon, github; 0 for all",
	"helpQuery":         "query",
	"helpWordsKey":      "words",
	"helpWords":         "whole words, the last one as a prefix: useTa finds useTabs",
	"helpTag":           "one tag from glane summarize at a time",
	"close":             "close",
	"copy":              "copy",
	"copied":            "copied",
	"copyFailed":        "copy blocked, here it is:",
	"countsFTS":         "counts full-text matches only",

	"resultsAria": "Results",
	"ranked":      "ranked by relevance",
	"newest":      "newest first",
	"bothTitle":   "Found by full-text and semantic search",
	"clearAll":    "Clear the query and filters",
	"result":      "result",
	"truncated":   "· capped, add a word or a filter",
	// %s is what the user typed after the operator
	"noteSince":    "· since:%s ignored, want 2026, 2026-07-20 or 7d",
	"noteSource":   "· unknown source:%s ignored",
	"noteRepeated": "· only %s counts, one per filter",
	"ftsTitle":     "Found by full-text search",
	"semTitle":     "Found by semantic search",
	"on":           "on",
	"noResults":    "No results. Try other keywords, switch source or widen the date.",

	"statsTitle":       "glane · stats",
	"statsDescription": "Statistics on your gleaned tech watch: items imported, enriched, summarized and vectorized.",
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
	// past a month reltime shows the date itself: Go's month names are English
	// only, so French gets the numeric form rather than a month table.
	"dateFmt": "2 Jan 2006",
}

var fr = catalog{
	"lang":              "fr",
	"title":             "glane · veille techno",
	"description":       "Recherche dans votre veille technologique : dépôts, articles et posts glanés au fil de l'eau.",
	"tagline":           "votre veille, retrouvée d'un mot",
	"stats":             "Stats",
	"searchPlaceholder": "mots   source:github   tag:go   since:30d",
	"searchAria":        "Rechercher dans la veille",
	"sourceAria":        "Filtrer par source",
	"allSources":        "toutes",
	"since":             "depuis",
	"sinceAria":         "N'afficher que les items de cette période",
	"tags":              "tags",
	"search":            "recherche",
	"pagesAria":         "Pages",
	"skipToResults":     "Aller aux résultats",
	"filters":           "filtres",
	"filtersAria":       "Filtres",
	"toggleFilters":     "Afficher ou masquer les filtres",
	"allTime":           "tout",
	"copyFix":           "Copier la commande qui complète l'index",
	"items":             "éléments",
	"off":               "désactivé",
	"keyMove":           "naviguer",
	"keyOpen":           "ouvrir",
	"keyCopy":           "copier l'url",
	"keyHelp":           "aide",
	"helpKeys":          "clavier",
	"helpFocus":         "aller à la recherche ; esc en sort sans l'effacer",
	"helpMove":          "résultat suivant / précédent",
	"helpOpen":          "ouvrir le lien ; O ouvre le post d'origine",
	"helpCopy":          "copier l'url ; Y copie un lien markdown",
	"helpSources":       "filtrer twitter, bluesky, mastodon, github ; 0 pour toutes",
	"helpQuery":         "requête",
	"helpWordsKey":      "mots",
	"helpWords":         "mots entiers, le dernier en préfixe : useTa trouve useTabs",
	"helpTag":           "un tag issu de glane summarize, un seul à la fois",
	"close":             "fermer",
	"copy":              "copier",
	"copied":            "copié",
	"copyFailed":        "copie bloquée, la voici :",
	"countsFTS":         "compte les correspondances plein texte",

	"resultsAria":  "Résultats",
	"ranked":       "classés par pertinence",
	"newest":       "plus récents d'abord",
	"bothTitle":    "Trouvé par les recherches plein texte et sémantique",
	"clearAll":     "Effacer la requête et les filtres",
	"result":       "résultat",
	"truncated":    "· liste plafonnée, ajoutez un mot ou un filtre",
	"noteSince":    "· since:%s ignoré, attendu 2026, 2026-07-20 ou 7d",
	"noteSource":   "· source:%s inconnue, ignorée",
	"noteRepeated": "· seul %s compte, un par filtre",
	"ftsTitle":     "Trouvé par la recherche plein texte",
	"semTitle":     "Trouvé par la recherche sémantique",
	"on":           "sur",
	"noResults":    "Aucun résultat. Essayez d'autres mots-clés, changez de source ou élargissez la date.",

	"statsTitle":       "glane · statistiques",
	"statsDescription": "Statistiques sur votre veille technologique glanée : items importés, enrichis, résumés et vectorisés.",
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
	"dateFmt": "02/01/2006",
}

// pick reads the browser's Accept-Language. Header-only means the htmx
// fragments follow the page with no cookie, no ?lang, no state to carry.
//
// Tags are case-insensitive and matched on the primary subtag, so "EN-US" is
// English and "frr" (Northern Frisian) is not French. q=0 means "not
// acceptable" and skips the tag.
//
// ponytail: takes the first acceptable fr/en tag in header order rather than
// sorting by q-value — browsers already send them in preference order. Pull in
// golang.org/x/text/language if a hand-written header ever needs to win.
func pick(w http.ResponseWriter, r *http.Request) catalog {
	// the response depends on the header: say so, or a cache is free to replay
	// one language at a reader who asked for the other
	w.Header().Set("Vary", "Accept-Language")
	for _, tag := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		name, params, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), ";")
		if q, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if v, err := strconv.ParseFloat(q, 64); err == nil && v == 0 {
				continue
			}
		}
		// re-trim: "fr ;q=0.9" is legal, the space stays on the name
		primary, _, _ := strings.Cut(strings.TrimSpace(name), "-")
		switch primary {
		case "fr":
			return fr
		case "en":
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
