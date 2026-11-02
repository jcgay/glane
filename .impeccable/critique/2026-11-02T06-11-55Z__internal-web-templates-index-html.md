---
target: glane web UI (index.html)
total_score: 27
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 3
target_identity: "file:/Volumes/sourcecode/golang/src/github.com/glane/internal/web/templates/index.html"
target_fingerprint: "sha256:f7381b17cd81e869fd2cb1a3456afb7217424cf53feb246eaaa5a1802647b669"
target_path: /Volumes/sourcecode/golang/src/github.com/glane/internal/web/templates/index.html
timestamp: 2026-11-02T06-11-55Z
slug: internal-web-templates-index-html
---
Method: dual-agent (A: design review · B: detector + browser)

Heuristics: 1:3 2:3 3:2 4:3 5:3 6:3 7:3 8:3 9:2 10:2 = 27/40 (Acceptable, edge of Good)

Specificity: authored for glane (query box as single source of truth, coloured filter words + wavy underline, `$ glane search` echo, mono = machine / sans = human, copyable fix commands, source squares). Weak spot: rediscovery side, "telling tags" are still the broad ones (automation 2139, architecture 2034).

Fixed since last run: "0 result", "Dernière synchro", one-line status bar, touch help button, /stats links back into index. Partial: paging exists but no total + 1000 cap; tag rail folded to 20 but 20 are generic and the new year list is a 16-item wall.

Priority issues
- [P1] Facet OOB swap drops keyboard focus to <body> (facets.html:8/20/39 hx-swap-oob replaces the pressed button; period buttons keep focus). Fix: remember data-op/data-v, refocus matching button in htmx:afterSettle (index.html:520).
- [P1] New search keeps old scroll offset (after paging, new query lands ~row 43). Fix: reset .list scrollTop / scrollTo(0,0) in afterSwap (index.html:509) when not appending, or hx-swap show:top.
- [P1] No total in header: "50 results · there are more" (results.html:3-4) while rail knows 396. Fix: "50 of 396"; reconsider 1000 cap (web.go:130).
- [P2] Phone filter panel ~630/844px, stays open after a tap, 16 years without counts (index.html:246). Fix: close on facet tap, collapse years to recent 4 + older, or one chip row.
- [P2] Telling tags not telling: broadShare 0.3 (web.go:159). Fix: rank by lift vs corpus or drop to ~0.1.

Personas: Alex (focus loss, no year/tag keys, 7× n to item 396, ~33 tab stops before results + 3-4 per hit). Sam (focus loss, skip link only via Shift+Tab due to autofocus, 16 year tab stops, notes styled like errors). Casey (autofocus pops keyboard, panel covers screen, /stats only via footer link, until: hidden in truncated box). Maintainer Sunday browse (generic tags, no year total, /stats cards lack enrich/summarize commands, mastodon:authorfeed 3.5 months stale with no warning — staleness per source, web.go:259).

Minor: hover overrides pressed on years (index.html:89 vs :90); year/count pairs ambiguous in two columns; removed filters leave runs of spaces (put(), index.html:458); "sync" hardcoded in FR status bar (index.html:382), rail "source"/"index" hardcoded; empty state always says "drop a filter" even with none; "0 results · ranked by relevance"; .rail .note 11px faint, appears only with a query and shifts rail 32px; "GitHub - owner/repo:" title prefix; "all" and github squares near-identical grey in light; .health below fold.

Detector: CLI exit 0, 5 advisories (10px/16px off-ramp at index.html:93/284 — 16px is iOS no-zoom, document it; rgba(0,0,0,.45) scrim at :215; two rgb(0,0,0) on fragments = false positive). URL: tiny-text on p.note (true, low). Config suppresses overused-font (deliberate), marquee (false: htmx load bar), cramped .seg/.card (false), flat-type-hierarchy (borderline true: 13/14/14px, 1.08 ratio).

Questions: why default to newest, not "a year ago this week"? why make the user guess the total? does the read-only phone need a filter panel at all?
