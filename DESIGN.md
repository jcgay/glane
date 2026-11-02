---
name: glane
description: A keyboard-first search console over your own tech-watch index.
colors:
  graphite-bg: "#121210"
  graphite-surface: "#18171a"
  graphite-surface-2: "#1f1e1c"
  graphite-border: "#2a2926"
  graphite-border-strong: "#3a3833"
  bone-ink: "#e9e6df"
  dust-muted: "#a29c91"
  dust-faint: "#959086"
  console-amber: "#f0a050"
  console-amber-ink: "#f6b877"
  console-amber-soft: "rgba(240, 160, 80, .12)"
  console-amber-sel: "rgba(240, 160, 80, .07)"
  signal-green: "#7cc48b"
  src-twitter: "#6ba3dd"
  src-bluesky: "#45a7c9"
  src-mastodon: "#a281e0"
  src-github: "#9aa2ad"
  paper-bg: "#f7f6f3"
  paper-surface: "#ffffff"
  paper-surface-2: "#f0eee9"
  paper-border: "#e4e0d8"
  paper-border-strong: "#d3cdc2"
  paper-ink: "#1c1b18"
  paper-muted: "#5f594f"
  paper-faint: "#655f55"
  burnt-amber: "#b45309"
  burnt-amber-ink: "#8a3f07"
  burnt-amber-soft: "#fbeddd"
  burnt-amber-sel: "#fdf4ea"
  paper-ok: "#2f7d3e"
  paper-src-twitter: "#4a86c6"
  paper-src-bluesky: "#0a7ea4"
  paper-src-mastodon: "#7a5bc0"
  paper-src-github: "#5b6470"
typography:
  display:
    fontFamily: "Geist, ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif"
    fontSize: "3.2rem"
    fontWeight: 600
    lineHeight: 1
    letterSpacing: "-.04em"
    fontFeature: "tnum"
  headline:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "1.6rem"
    fontWeight: 600
    letterSpacing: "-.02em"
    fontFeature: "tnum"
  title:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14.5px"
    fontWeight: 560
    lineHeight: 1.35
    letterSpacing: "-.01em"
  body:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.45
  body-small:
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13.5px"
    fontWeight: 400
    lineHeight: 1.45
  brand:
    fontFamily: "Geist Mono, ui-monospace, SF Mono, Menlo, monospace"
    fontSize: "15px"
    fontWeight: 600
    letterSpacing: "-.02em"
  command:
    fontFamily: "Geist Mono, ui-monospace, SF Mono, Menlo, monospace"
    fontSize: "14px"
    fontWeight: 400
    letterSpacing: "0"
  meta:
    fontFamily: "Geist Mono, ui-monospace, SF Mono, Menlo, monospace"
    fontSize: "12px"
    fontWeight: 400
    fontFeature: "tnum"
  label:
    fontFamily: "Geist Mono, ui-monospace, SF Mono, Menlo, monospace"
    fontSize: "11px"
    fontWeight: 500
    letterSpacing: ".08em"
rounded:
  hair: "2px"
  xs: "3px"
  sm: "4px"
  control: "5px"
  md: "6px"
  lg: "8px"
  dialog: "10px"
  card: "0.8rem"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "20px"
  touch: "44px"
components:
  command-bar:
    backgroundColor: "{colors.graphite-bg}"
    textColor: "{colors.bone-ink}"
    typography: "{typography.command}"
    rounded: "{rounded.lg}"
    padding: "0 10px"
  button:
    textColor: "{colors.dust-muted}"
    typography: "{typography.meta}"
    rounded: "{rounded.sm}"
    padding: "1px 6px"
  button-hover:
    textColor: "{colors.console-amber-ink}"
  button-touch:
    height: "{spacing.touch}"
    padding: "0 16px"
  facet-row:
    textColor: "{colors.dust-muted}"
    rounded: "{rounded.control}"
    padding: "4px 8px"
  facet-row-pressed:
    backgroundColor: "{colors.console-amber-soft}"
    textColor: "{colors.console-amber-ink}"
  segmented-control:
    textColor: "{colors.dust-muted}"
    typography: "{typography.meta}"
    rounded: "{rounded.md}"
    padding: "5px 0"
  tag-pill:
    backgroundColor: "{colors.graphite-surface-2}"
    textColor: "{colors.dust-muted}"
    typography: "{typography.meta}"
    rounded: "{rounded.sm}"
    padding: "2px 6px"
  tag-pill-pressed:
    backgroundColor: "{colors.console-amber-soft}"
    textColor: "{colors.console-amber-ink}"
  result-row:
    textColor: "{colors.bone-ink}"
    padding: "12px 20px"
  result-row-selected:
    backgroundColor: "{colors.console-amber-sel}"
  provenance-badge:
    textColor: "{colors.dust-faint}"
    rounded: "{rounded.xs}"
    padding: "0 4px"
  provenance-badge-vec:
    textColor: "{colors.console-amber-ink}"
  kbd:
    backgroundColor: "{colors.graphite-surface-2}"
    textColor: "{colors.dust-muted}"
    rounded: "{rounded.sm}"
    padding: "0 .35em"
  stat-card:
    backgroundColor: "{colors.graphite-surface}"
    rounded: "{rounded.card}"
    padding: "1rem 1.1rem 1.1rem"
  status-bar:
    backgroundColor: "{colors.graphite-surface}"
    textColor: "{colors.dust-faint}"
    typography: "{typography.meta}"
    padding: "6px 16px"
  help-dialog:
    backgroundColor: "{colors.graphite-surface}"
    textColor: "{colors.bone-ink}"
    rounded: "{rounded.dialog}"
    padding: "20px 24px"
  toast:
    backgroundColor: "{colors.bone-ink}"
    textColor: "{colors.graphite-bg}"
    typography: "{typography.meta}"
    rounded: "{rounded.md}"
    padding: "6px 12px"
---

# Design System: glane

## Overview

**Creative North Star: "The Gleaner's Ledger"**

glane comes from *glaner*, to glean: the UI is the working ledger of what you picked up along the way, kept open beside the terminal. A ledger is dense, ruled and plain; every line is an entry you can find again, and nothing on the page is there for show.

glane's web UI is a dense, dark-first search console meant to sit next to a terminal and an editor. It is graphite with a faint amber bias, and amber is the only accent. One screen holds a top bar with the command line, a filter rail, a scrolling result list and a status bar; only the list and the rail scroll. Everything is reachable from the keyboard, and the interface keeps saying how to drive it: keycaps next to the controls, a `$ glane …` echo of the current query, a help dialog on `?`.

Density is deliberate. The working type sits between 11px and 15px, and hierarchy comes from weight, from mono against sans, and from colour, not from size jumps. Mono carries anything the machine says (commands, counts, dates, provenance, labels). Sans carries anything a human wrote (titles, summaries). Light mode is a warm paper version of the same system, picked through `prefers-color-scheme`. Fonts and assets are embedded so the UI looks the same offline, in the browser, in the desktop window and in the Android wrapper.

**Key Characteristics:**
- Dark first; light is a full token override, never a separate design.
- One accent (amber), used for focus, the caret, selection, pressed state and the loading bar.
- Mono for machine output, sans for human text.
- Flat surfaces separated by 1px borders; the one shadow belongs to the help dialog.
- Keyboard-first, with a 44px touch floor that only applies under `pointer: coarse`.

## Colors

A near-black warm graphite (or a warm paper in light mode), bone ink, dusty greys, one amber, plus four muted source hues that only ever appear as small squares.

### Primary
- **Console Amber** (`console-amber`, light: `burnt-amber`): the single accent. Focus rings, the caret, the command bar's focused border, the brand square, the loading bar, meter fills and the wavy underline under an invalid filter.
- **Amber Ink** (`console-amber-ink`, light: `burnt-amber-ink`): amber used as text. Filter operators in the command bar, pressed facets and tags, links in the meta row, "has more" notes, stale-sync warnings.
- **Amber Wash** (`console-amber-soft`, light: `burnt-amber-soft`): the background of pressed controls, search-hit `<mark>`, text selection and the command bar's 3px focus halo.
- **Amber Haze** (`console-amber-sel`, light: `burnt-amber-sel`): the background of the j/k-selected result row. It is fainter than the wash so a selected row never reads as a pressed control.

### Tertiary
- **Source hues** (`src-twitter` steel blue, `src-bluesky` lagoon, `src-mastodon` lilac, `src-github` slate; each has a `paper-` light variant): they only fill the 8–10px source squares in the rail, the result gutter and the stats tables. They never colour text or surfaces.
- **Signal Green** (`signal-green`, light: `paper-ok`): only the "on" state of the fts5/vec engines in the status bar.

### Neutral
- **Graphite** (`graphite-bg` / `paper-bg`): page background, and the inset background of the command bar.
- **Raised Graphite** (`graphite-surface` / `paper-surface`): top bar, status bar, CLI echo, stat cards, dialog, the phone rail panel.
- **Graphite Well** (`graphite-surface-2` / `paper-surface-2`): hover rows, tag pills, keycaps, empty meter tracks.
- **Hairline** (`graphite-border` / `paper-border`): every structural divider, between rows, panes and bars.
- **Strong Hairline** (`graphite-border-strong` / `paper-border-strong`): borders of small controls (buttons, keycaps, provenance badges, segment dividers, the dialog) and the scrollbar thumb.
- **Bone Ink** (`bone-ink` / `paper-ink`): titles and primary text.
- **Dust** (`dust-muted` / `paper-muted`): summaries, unpressed controls, secondary values.
- **Faint Dust** (`dust-faint` / `paper-faint`): rail labels, counts, dates, gutter numbers, placeholders, and the outline of the command bar and segmented control.

### Named Rules
**The One Amber Rule.** Amber is the only accent. Every interactive emphasis (focus, pressed, selected, active link, progress) is a tone of it. No second accent hue.

**The 4.5 Floor Rule.** Faint Dust is the darkest grey that still reads at 4.5:1 on every surface, selected rows included. No text goes below it. That is why muted and faint sit close together: faint is a contrast floor, not a decorative tint.

**The Square Signature Rule.** A source is identified by a small rounded square in its hue (8px, 10px in the result gutter, 2px radius) next to its lowercase name. Source hues never fill text or areas. "All sources" is the same square drawn hollow, a 1.5px Faint Dust outline.

## Typography

**Display Font:** Geist (embedded variable woff2, falling back to ui-sans-serif, system-ui)
**Body Font:** Geist
**Label/Mono Font:** Geist Mono (embedded variable woff2, falling back to ui-monospace, SF Mono, Menlo)

**Character:** a neutral grotesk for what people wrote and its mono twin for what the machine says. The pairing splits the interface into prose and instrument readout without a second family.

### Hierarchy
- **Display** (600, 3.2rem, line-height 1, -.04em, tabular): only the corpus total on /stats. Everything else is read against it.
- **Headline** (600, 1.6rem, -.02em, tabular): the counts on stat cards.
- **Title** (560, 14.5px, 1.35, -.01em, balanced wrap, clamped to 2 lines unless the row is selected): result titles. 15.5px on phones.
- **Body** (400, 14px, 1.45): page default and dialog text. Result summaries are 13.5px Dust, capped at 56ch and clamped to 2 lines (14.5px on phones).
- **Brand** (Mono 600, 15px, -.02em): the "glane" wordmark.
- **Command** (Mono 400, 14px): the query field and its coloured overlay; 16px under `pointer: coarse` so mobile browsers don't zoom.
- **Meta** (Mono 400, 12–13px, tabular numbers): counts, dates, the meta row, status bar, nav links (13px, lowercase), buttons, tags, the segmented control.
- **Label** (Mono 500, 11px, uppercase, .08em): rail and stats section headings, which name the control or table below them. Keycaps use Mono 500 11px (10px in the rail).

### Named Rules
**The Mono Means Machine Rule.** Commands, counts, dates, sources, provenance and keys are Geist Mono. Titles and summaries are Geist. Never swap them.

**The Tight Range Rule.** Console surfaces stay within 11–15px. Make hierarchy with weight (400/500/560/600), with family and with Ink/Dust/Faint, not with bigger sizes. Only /stats has big numbers.

**The Tabular Numbers Rule.** Every number that sits in a column or updates (counts, gutter indexes, dates, percentages) uses `font-variant-numeric: tabular-nums`.

## Layout

The search page is a full-height flex column: top bar, a grid shell (a 224px rail and a `minmax(0, 1fr)` list, which become one column when the rail is folded with `[`), and a status bar. Only the rail and the list scroll. Result rows are a three-column grid: a 3ch gutter (index number over the source square), the content, and an 11ch date column. Summary, excerpt and meta rows span the content and date columns.

/stats uses a centred 780px column (`.wrap`, 1.25rem side padding, 2rem top and 5rem bottom padding). Cards sit in an `auto-fit, minmax(140px, 1fr)` grid with a .8rem gap; the total spans the full row. Tables pair a 12rem label, a meter and a 6rem right-aligned mono count.

Rhythm runs on 4/8/12/16/20px. Rows are padded 12px 20px, the rail has 16px padding with 22px between sections, bars use 6–12px vertical padding.

**Phones (≤760px):** the document scrolls (so the URL bar collapses and pull-to-refresh works) and the top bar becomes sticky. The rail starts folded and opens as a panel under the bar, with the period first and its years on one row, then sources two by two, then tags on two rows; the year and tag rows scroll sideways with a 40px fade on the right. A tap on a facet folds the panel again. The rail health block and nav links are hidden; the panel ends with a stats link instead. A touch screen never focuses the search box on load, so no keyboard rises over the results.

**Touch (`pointer: coarse`, any width):** every tappable control is at least 44px tall. Rail tags are 32px, result tags 24px with an invisible hit area that brings them to 34px. Keycap hints, the key legend and the CLI echo are hidden, and the help dialog explains taps instead of keys.

### Named Rules
**The One Screen Rule.** On a desktop the page never scrolls; the panes do.

**The Touch Floor Rule.** Size for fingers only under `pointer: coarse`. A mouse keeps the dense console.

## Elevation & Depth

Flat. Depth comes from tonal layering (Graphite page, Raised Graphite bars and cards, Graphite Well for hover and wells) and 1px hairlines. The only shadow is on the modal help dialog, over a 45% black backdrop. Focus is shown with an outline or a halo, never with a shadow.

### Shadow Vocabulary
- **Dialog lift** (dark: `box-shadow: 0 24px 48px -16px rgba(0, 0, 0, .5)`; light: `0 1px 2px rgba(60, 50, 30, .05), 0 16px 40px -16px rgba(60, 50, 30, .25)`): the help dialog only.
- **Focus halo** (`box-shadow: 0 0 0 3px` Amber Wash): the command bar when it has focus.

### Named Rules
**The Flat Console Rule.** Bars, rows, rail and cards don't cast shadows. A modal is the only thing that floats.

## Shapes

Small, tight radii that grow with the size of the element: 2px for dots, meters and marks, 3px for provenance badges, 4px for keycaps, tags, buttons and the focus ring, 5px for rail rows and icon buttons, 6px for the segmented control, CLI echo and toast, 8px for the command bar (and touch icon buttons), 10px for the dialog, .8rem for stat cards. Borders are always 1px, except the keycap, whose 2px bottom edge makes it look like a physical key. The brand mark and the source markers are rounded squares, not circles.

## Components

Precise and quiet. Controls are drawn with hairlines and stay unfilled until pressed; the keycap next to a control is the only ornament. A new component earns its place by doing one job the keyboard can reach.

### Command Bar
The core of the console. It has a Graphite inset background, a 1px Faint Dust outline and an 8px radius, with an amber chevron icon on the left and a `/` keycap on the right. The typed text is drawn by an overlay underneath a transparent input: filter operators (`source:`, `tag:`, `since:`, `until:`) are Amber Ink with a faint colon, and a value the server will ignore gets an amber wavy underline. With `forced-colors` the overlay is dropped and the input text shows. On focus the outline turns amber and gets the 3px Amber Wash halo. A 2px amber loading bar slides along the bottom edge of the top bar while a request runs, and the stale results drop to 55% opacity.

### Buttons
- **Shape:** gently squared (4px), 1px Strong Hairline border, Mono 11px Dust.
- **Hover:** Amber Ink text and an amber border. No fill.
- **Larger use:** "more" is padded 4px 12px at 12px. Under touch every button is 44px tall with 0 16px padding.
- **Icon buttons** (fold, help): borderless, Faint Dust, 5px radius, Graphite Well and Ink on hover. Under touch they become 44px squares. On phones a pressed fold button is Amber Wash, and a 7px amber square says a filter is on.

### Chips (tag pills)
- **Style:** Graphite Well, a 1px Hairline border, 4px radius, Mono 12px Dust, with a Faint `#` before the name. Rail tags show a count.
- **State:** hover gives Amber Ink text and an amber border. Pressed adds Amber Wash. The rail's tags are one Tab stop and arrow keys move between them. Results show three tags, then a `+n`.

### Facet rows and segmented control
Rail facet rows are full-width buttons (13px, Dust, 5px radius) laid out as source square, name, tabular count and keycap. Hover is Graphite Well, pressed is Amber Wash with Amber Ink, disabled is 50% opacity. The period control is a 4-cell segmented control: a 1px Faint Dust outline (3:1 against the rail), 6px radius, Strong Hairline dividers, Mono 12px, and pressed cells in Amber Wash. Years sit under it in a two-column facet grid, each count right after its year. A caveat on a section ("full-text counts") sits on its heading's line, Mono 400 without the uppercase, so it never pushes the rows down.

### Result Row
A numbered, ruled row. It has the gutter (Faint index number turning amber when selected, source square), a Title in Ink, a time in Faint Mono, a summary in Dust, an optional matched excerpt behind a 1px left rule, and a Mono meta row: host link in Amber Ink with an arrow icon, "on source" link, author and source/kind, a provenance badge, tags. Hover is Graphite Well. The j/k-selected row is Amber Haze, unclamps its title and summary, and gets a 2px inset amber outline when its title link has focus. Search hits are `<mark>`ed in Amber Wash with Amber Ink.

### Provenance Badge
A Mono 11px tag with a 3px radius and a 1px border that says where a hit came from: `bm25` has a Strong Hairline border, `vec` and `bm25+vec` are Amber Ink with an amber border.

### Keycap
Mono 500 11px Dust on Graphite Well, 4px radius, 1px Strong Hairline border with a 2px bottom edge. It sits next to whatever control it triggers, and in the status bar legend and the help dialog.

### Navigation
The top bar is Raised Graphite with a Hairline bottom border and 12px 16px padding. The brand is an 8px amber rounded square plus "glane" in Mono 600. Nav links are Mono 13px, lowercase, Dust, and turn Ink on hover or when current. The status bar mirrors the top bar at the bottom: Mono 12px Faint, engine states, sync age (Amber Ink when stale) and the key legend on the right.

### Stats Cards and Meters
Cards are Raised Graphite with a Hairline border, .8rem radius and 1rem 1.1rem padding, holding a Headline count, a .84rem Dust label and a 4px meter (Graphite Well track, amber fill). The total is a bare Display number with no card. In tables the meters are Dust at 45% opacity so the amber stays on the cards. Every table row links back to the matching search.

### Help Dialog and Toast
The native `<dialog>` is 460px max, Raised Graphite, Strong Hairline border, 10px radius, dialog lift. Inside are Mono 600 14px headings and a two-column list of keycaps (right-aligned) against Dust descriptions. The toast is inverted (Ink background, Graphite text), Mono 12px, 6px radius, centred 48px above the bottom, and stays for 1.4s.

## Do's and Don'ts

### Do:
- **Do** take every colour from the `:root` custom properties in `base.css` so the light override follows for free.
- **Do** use Faint Dust (`--faint`) as the lightest text you allow; it is the 4.5:1 floor.
- **Do** set machine output (counts, dates, commands, sources, keys) in Geist Mono with tabular numbers, and human text in Geist.
- **Do** show the key next to any keyboard-driven control with a keycap, and hide it under `pointer: coarse`.
- **Do** express pressed or selected state with Amber Wash plus Amber Ink, and focus with the 2px amber outline (2px offset).
- **Do** keep touch targets at 44px (tags 32px, result tags 24px plus an invisible hit area) under `pointer: coarse` only.
- **Do** use the shared rule from `base.css` to zero transitions and animations under `prefers-reduced-motion`.

### Don't:
- **Don't** add a second accent hue; status green is reserved for "on" engines.
- **Don't** use source hues for text, fills or borders; only the small rounded square.
- **Don't** grow the console's type past 15px for hierarchy; use weight, family and Ink/Dust/Faint.
- **Don't** put shadows on bars, rows, rail or cards; only the modal dialog floats.
- **Don't** load fonts or scripts from a CDN; every asset is embedded so the UI renders offline.
