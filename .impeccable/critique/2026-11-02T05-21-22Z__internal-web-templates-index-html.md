---
target: glane web UI (index.html)
total_score: 26
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 3
target_identity: "file:/Volumes/sourcecode/golang/src/github.com/glane/internal/web/templates/index.html"
target_fingerprint: "sha256:0adef7ffe351929360e5a340bad0d27e0edbaad3e400fc9550d8d7828b7e2270"
target_path: /Volumes/sourcecode/golang/src/github.com/glane/internal/web/templates/index.html
timestamp: 2026-11-02T05-21-22Z
slug: internal-web-templates-index-html
---
Method: dual-agent (A: design review · B: detector + browser)

Heuristics: 1:3 2:3 3:3 4:3 5:2 6:2 7:3 8:3 9:2 10:2 = 26/40 (Acceptable)

Specificity: behaviour authored for glane (query-as-filter shared by CLI/rail/URL, `$ glane search` echo, copyable fix commands, bm25/vec badges); skin is stock dev console (graphite + amber, Geist, uppercase mono labels, KPI-card /stats). Nothing visual says gleaning/veille.

Previous run fixed: help <dialog>, listbox misuse, Esc wiping query, --faint contrast (now 5.9:1; muted/faint tiers nearly merged).

Priority issues
- [P1] Rediscovery capped at 50: results.html:4 "truncated" with no "more"; since: only rolling windows, no year/month browsing. Fix: "50 more" + n key (hx-swap beforeend + offset), year chips or until:.
- [P1] Tag rail unbounded and broken: facets.html:21 renders 518 buttons (~536 tab stops before results); .tag (index.html:111) wraps multi-word pills; "software engineering" covers 4011/8306; active tag hidden behind +N on rows; counts not scoped. Fix: top ~20 + disclosure/filter, roving tabindex, nowrap, active tag first, demote >30% tags.
- [P1] Help keyboard-only: index.html:351-354 hint spans, hidden on coarse pointer (index.html:263); phone placeholder truncated. Fix: real "? aide" button visible on touch, short phone placeholder.
- [P2] Copy/i18n: "0 result" (results.html:3 plural only >1), notes run together (results.html:5), "Dernier sync", FR number grouping, help capitalisation (index.html:365), invalid operators painted valid.
- [P2] Status bar: 7 sync lines over 3 rows, no stale warning (mastodon 3.5 months). Fix: one line + stale warning, details on /stats.
- [P3] /stats generic dead end: no links to /?q=source:x, no tag/period distribution, no keys.

Personas: Alex (no paging key, no tag completion, no clear-filter key, /stats keyless). Sam (536 tab stops, role=status inside swapped fragment, no h1, light seg borders 1.46:1). Casey (no help, cap 50, two taps to open undiscoverable). Maintainer Sunday browse (generic tags, unscoped counts, no oldest/random/period).

Minor: "GitHub - " title prefix, wide mono relative times, tagline only in tooltip, Twitter absent from sync list without explanation, hit h3 no text-wrap balance, .hit p ~110 chars/line (78ch on proportional font), .tag padding 3.8px.

Detector: CLI 1 (cramped-padding stats.html .card.total, false positive); browser/URL line-length 42-47 (true, low), cramped-padding .tag 15 (borderline true, hidden in CLI by broad index.html ignore), unbalanced-heading 2 (true, advisory), tiny-text .rail .note (false positive), overused-font (deliberate). No contrast findings.
