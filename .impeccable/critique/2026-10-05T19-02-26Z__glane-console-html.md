---
target: glane console mockup
total_score: 27
max_score: 40
na_heuristics: 
p0_count: 1
p1_count: 3
timestamp: 2026-10-05T19-02-26Z
slug: glane-console-html
---
Method: dual-agent (A: design review · B: detector + browser)

Heuristics: 1:3 2:4 3:1 4:2 5:2 6:3 7:4 8:3 9:2 10:3 = 27/40 (Acceptable)

Specificity: mostly authored for glane (CLI echo, bm25/vec provenance, index health, query operators mapping to CLI flags); the shell (command bar + rail + status bar, mono, amber on graphite) is stock dev-console.

Priority issues
- [P0] Help dialog: .help{display:grid} beats [hidden] outside the artifact skeleton; use <dialog> + showModal/close, restore focus.
- [P1] listbox/option misuse: not focusable, no aria-activedescendant, option children presentational; switch to ol>article with real focus on j/k, role=status on count.
- [P1] Esc in input[type=search] wipes query natively; preventDefault on Escape, hide native cancel button.
- [P1] --faint below AA (dark 4.16 on bg/3.68 on sel, light 4.34); cmd border 1.6:1 < 3:1.
- [P2] Regressions vs current UI: truncated-at-50 marker, loading bar, excerpt, original-post link (O), i18n, active tag state, disabled zero-count sources, tag counts not filtered.

Personas: Alex (Esc destroys query, single tag:, substring highlight vs FTS token match, click doesn't open). Sam (no dialog semantics, listbox swallows links, forced-colors breaks transparent input, no skip link).

Minor: fixed-width date cell, CSS clamp for untitled h3, cap tags +n, mobile rail/status eat 300px and inner scroll, fake 4ms, weight 550 not loaded.

Detector: overused-font (Geist, deliberate, false positive), low-contrast (true), line-length (overlay label, false positive), flat-type-hierarchy 11-15px (borderline, dense console intent).
