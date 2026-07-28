# glane stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `glane stats` CLI command and a `/stats` web page that both report a
point-in-time snapshot of the indexed data: totals, per-source breakdown, enrichment/
summarization/embedding coverage, distinct tag count, and per-source last-sync time.

**Architecture:** A single new `Store.Stats()` method in `internal/store` computes all
aggregates from existing tables (`items`, `embeddings`, `item_tags`, `sync_state`) with
plain SQL — no schema changes. `main.go` gets a `cmdStats` wrapper (text or `-json`
output, English only). `internal/web` gets a `/stats` route rendering a new
`stats.html` template (French, matching the rest of the web UI) plus a nav link from
the home page.

**Tech Stack:** Go, `database/sql` over `modernc.org/sqlite`, Go `html/template`, `flag`,
`encoding/json`.

## Global Constraints

- No schema changes — `Stats()` only reads existing tables (spec: "Modèle de données").
- `Stats()` returns a real `error`; it does not degrade silently itself. Callers decide:
  CLI uses `fatal`, web logs and renders zero-value stats (spec: "Notes d'implémentation").
- CLI text output is English-only; the web page stays French, matching the rest of the
  UI (spec: "Commande CLI" note on language; user instruction 2026-07-28).
- CLI `-json` output uses `store.Stats` marshaled with default Go field names — no
  custom JSON tags (spec: "Commande CLI").
- `main.go`'s relative-time formatting for the CLI (`relTime`) is a separate, English-
  language implementation from `internal/web`'s French `reltime` — do not share/import
  one from the other (spec: "Aide au temps relatif (CLI, en anglais)").
- Update `README.md` and `completions/glane.fish` for the new `stats` command and
  `-json` flag (AGENTS.md convention).
- No new CLI flags beyond `-json` (no `-source` filter, etc.) — YAGNI (spec: "Hors périmètre").

---

### Task 1: `Store.Stats()` — aggregate query

**Files:**
- Create: `internal/store/stats.go`
- Test: `internal/store/stats_test.go`

**Interfaces:**
- Consumes: `s.db *sql.DB` (existing `Store` field), existing schema tables `items`,
  `embeddings`, `item_tags`, `sync_state`; existing test helpers `seedEnriched(t, s,
  id int, src, txt string)` and `itoa(i int) string` from `internal/store/summary_test.go`
  (same package, no import needed); existing `Store.SaveSummary(id int64, summary
  string, tags []string) error`, `Store.SaveEmbedding(id int64, model string, v
  []float32) error`, `Store.SetCursor(source, cursor string) error`, `Store.Upsert(items
  []Item) (int, error)`.
- Produces: `type SourceCount struct { Source string; Count int }`, `type SourceSync
  struct { Source string; UpdatedAt int64 }`, `type Stats struct { Total int; BySource
  []SourceCount; Enriched int; Summarized int; Embedded int; DistinctTags int;
  LastSyncBySource []SourceSync }`, `func (s *Store) Stats() (Stats, error)` — consumed
  by Task 2 (`cmdStats` in `main.go`) and Task 3 (`/stats` web handler).

- [ ] **Step 1: Write the failing tests**

Create `internal/store/stats_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestStatsZeroOnEmptyDB(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 0 || len(st.BySource) != 0 || st.Enriched != 0 || st.Summarized != 0 ||
		st.Embedded != 0 || st.DistinctTags != 0 || len(st.LastSyncBySource) != 0 {
		t.Fatalf("expected all-zero stats on empty db, got %+v", st)
	}
}

func TestStatsAggregatesAcrossSources(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// twitter: 2 items — one fully enriched+summarized+tagged+embedded, one plain.
	seedEnriched(t, s, 1, "twitter", "kube post")
	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "9", Kind: "like", Text: "plain", URL: "http://x/9"}}); err != nil {
		t.Fatal(err)
	}
	// github: 1 item, enriched but never summarized.
	seedEnriched(t, s, 2, "github", "repo post")

	var id1 int64
	if err := s.db.QueryRow(`SELECT id FROM items WHERE source=? AND source_id=?`, "twitter", itoa(1)).Scan(&id1); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSummary(id1, "a summary", []string{"kubernetes", "devops"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEmbedding(id1, "m", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor("github", "cursor-1"); err != nil {
		t.Fatal(err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 3 {
		t.Fatalf("Total = %d, want 3", st.Total)
	}
	want := map[string]int{"twitter": 2, "github": 1}
	if len(st.BySource) != 2 {
		t.Fatalf("BySource = %+v, want 2 entries", st.BySource)
	}
	for _, sc := range st.BySource {
		if want[sc.Source] != sc.Count {
			t.Fatalf("BySource[%s] = %d, want %d", sc.Source, sc.Count, want[sc.Source])
		}
	}
	if st.Enriched != 2 {
		t.Fatalf("Enriched = %d, want 2", st.Enriched)
	}
	if st.Summarized != 1 {
		t.Fatalf("Summarized = %d, want 1", st.Summarized)
	}
	if st.Embedded != 1 {
		t.Fatalf("Embedded = %d, want 1", st.Embedded)
	}
	if st.DistinctTags != 2 {
		t.Fatalf("DistinctTags = %d, want 2", st.DistinctTags)
	}
	if len(st.LastSyncBySource) != 1 || st.LastSyncBySource[0].Source != "github" {
		t.Fatalf("LastSyncBySource = %+v, want one entry for github", st.LastSyncBySource)
	}
	if st.LastSyncBySource[0].UpdatedAt == 0 {
		t.Fatal("expected non-zero UpdatedAt for github sync")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/store/ -run TestStats -v`
Expected: FAIL — `s.Stats undefined (type *Store has no field or method Stats)`

- [ ] **Step 3: Write the implementation**

Create `internal/store/stats.go`:

```go
package store

// SourceCount is the number of items for one source.
type SourceCount struct {
	Source string
	Count  int
}

// SourceSync is the last sync_state.updated_at recorded for one source. Only
// sources that have actually run a live sync appear here — a one-shot import
// source (twitter) never has a sync_state row.
type SourceSync struct {
	Source    string
	UpdatedAt int64 // unix seconds
}

// Stats is a point-in-time snapshot of what's indexed, computed on read.
type Stats struct {
	Total            int
	BySource         []SourceCount
	Enriched         int // items with fetch_status = 'ok'
	Summarized       int // items with article_summary != ''
	Embedded         int // distinct item_id in embeddings
	DistinctTags     int // distinct tag in item_tags
	LastSyncBySource []SourceSync
}

// Stats aggregates counts across items, embeddings, item_tags and sync_state.
// It returns a real error like other store methods (SearchFTS, TagCounts) —
// callers decide how to degrade (CLI: fatal; web: log + zero-value render).
func (s *Store) Stats() (Stats, error) {
	var st Stats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&st.Total); err != nil {
		return Stats{}, err
	}

	srcRows, err := s.db.Query(`SELECT source, COUNT(*) c FROM items GROUP BY source ORDER BY c DESC, source`)
	if err != nil {
		return Stats{}, err
	}
	for srcRows.Next() {
		var sc SourceCount
		if err := srcRows.Scan(&sc.Source, &sc.Count); err != nil {
			srcRows.Close()
			return Stats{}, err
		}
		st.BySource = append(st.BySource, sc)
	}
	if err := srcRows.Err(); err != nil {
		srcRows.Close()
		return Stats{}, err
	}
	srcRows.Close()

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE fetch_status = 'ok'`).Scan(&st.Enriched); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE article_summary != ''`).Scan(&st.Summarized); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT item_id) FROM embeddings`).Scan(&st.Embedded); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT tag) FROM item_tags`).Scan(&st.DistinctTags); err != nil {
		return Stats{}, err
	}

	syncRows, err := s.db.Query(`SELECT source, updated_at FROM sync_state ORDER BY source`)
	if err != nil {
		return Stats{}, err
	}
	defer syncRows.Close()
	for syncRows.Next() {
		var ss SourceSync
		if err := syncRows.Scan(&ss.Source, &ss.UpdatedAt); err != nil {
			return Stats{}, err
		}
		st.LastSyncBySource = append(st.LastSyncBySource, ss)
	}
	return st, syncRows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/store/ -run TestStats -v`
Expected: PASS (both `TestStatsZeroOnEmptyDB` and `TestStatsAggregatesAcrossSources`)

- [ ] **Step 5: Run the full store package test suite (no regressions)**

Run: `mise exec -- go test ./internal/store/`
Expected: PASS (`ok  	github.com/jcgay/glane/internal/store	...`)

- [ ] **Step 6: Commit**

```bash
git add internal/store/stats.go internal/store/stats_test.go
git commit -m "feat(store): add Stats() aggregate query"
```

---

### Task 2: `glane stats` CLI command

**Files:**
- Modify: `main.go` (usage string, `switch os.Args[1]` dispatch, new `cmdStats`,
  `renderStats`, `relTime` functions)
- Modify: `README.md` (new `### glane stats [-json]` section)
- Modify: `completions/glane.fish` (new subcommand + `-json` flag)

**Interfaces:**
- Consumes: `store.Stats` and `func (s *store.Store) Stats() (store.Stats, error)`
  from Task 1; existing `fatal(err error)` helper already defined in `main.go`.
- Produces: `func cmdStats(s *store.Store, args []string)`, `func renderStats(st
  store.Stats) string`, `func relTime(ts int64) string` — none consumed by later
  tasks (CLI and web render independently), but must not collide with any existing
  `main.go` identifier.

- [ ] **Step 1: Add the `encoding/json` import**

In `main.go`, extend the import block:

```go
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/bluesky"
	"github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/enrich"
	"github.com/jcgay/glane/internal/github"
	"github.com/jcgay/glane/internal/mastodon"
	"github.com/jcgay/glane/internal/search"
	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/summarize"
	"github.com/jcgay/glane/internal/twitter"
	"github.com/jcgay/glane/internal/web"
)
```

- [ ] **Step 2: Wire the usage string and dispatch**

Find in `main.go`:

```go
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|summarize|update|tags|serve|version> ...")
		os.Exit(2)
	}
```

Replace with:

```go
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|summarize|update|tags|stats|serve|version> ...")
		os.Exit(2)
	}
```

Find in `main.go`:

```go
	case "tags":
		cmdTags(s)
	default:
```

Replace with:

```go
	case "tags":
		cmdTags(s)
	case "stats":
		cmdStats(s, os.Args[2:])
	default:
```

- [ ] **Step 3: Add `cmdStats`, `renderStats`, and `relTime`**

Add these functions right after `cmdTags` in `main.go` (near the existing `func
cmdTags(s *store.Store) { ... }`):

```go
func cmdStats(s *store.Store, args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Parse(args)

	st, err := s.Stats()
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(st); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Print(renderStats(st))
}

// renderStats formats a Stats snapshot as aligned, English-language text —
// CLI output stays English even though the web UI is French.
func renderStats(st store.Stats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Total          %d items\n", st.Total)
	for _, sc := range st.BySource {
		fmt.Fprintf(&b, "  %-12s %d\n", sc.Source, sc.Count)
	}
	fmt.Fprintf(&b, "Enriched        %d / %d\n", st.Enriched, st.Total)
	fmt.Fprintf(&b, "Summarized      %d / %d\n", st.Summarized, st.Total)
	fmt.Fprintf(&b, "Embeddings      %d\n", st.Embedded)
	fmt.Fprintf(&b, "Tags            %d distinct\n", st.DistinctTags)
	if len(st.LastSyncBySource) > 0 {
		b.WriteString("Last sync\n")
		for _, ss := range st.LastSyncBySource {
			fmt.Fprintf(&b, "  %-12s %s\n", ss.Source, relTime(ss.UpdatedAt))
		}
	}
	return b.String()
}

// relTime renders a unix timestamp as a short English relative age, or
// "never" for a zero/absent timestamp. This mirrors internal/web's French
// reltime template func in logic only — it is a separate implementation
// because CLI output stays English-only while the web UI stays French.
func relTime(ts int64) string {
	if ts <= 0 {
		return "never"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return time.Unix(ts, 0).Format("2006-01-02")
	}
}
```

- [ ] **Step 4: Build and smoke-test manually**

Run: `mise exec -- go build -o /tmp/glane-stats-check .`
Expected: builds with no errors.

Run:
```sh
export GLANE_DB=/tmp/glane-stats-check.db
/tmp/glane-stats-check import twitter testdata/nonexistent 2>/dev/null; true
/tmp/glane-stats-check stats
```
Expected: prints `Total          0 items`, `Enriched        0 / 0`, `Summarized
0 / 0`, `Embeddings      0`, `Tags            0 distinct` with no `Last sync`
section (empty DB — no crash, no panic).

Run: `/tmp/glane-stats-check stats -json`
Expected: pretty-printed JSON with keys `Total`, `Enriched`, `Summarized`,
`Embedded`, `DistinctTags` all `0`, and `BySource`/`LastSyncBySource` as `null`.

Clean up: `rm -f /tmp/glane-stats-check /tmp/glane-stats-check.db`

- [ ] **Step 5: `go vet` stays clean**

Run: `mise exec -- go vet ./...`
Expected: no output (clean).

- [ ] **Step 6: Update `README.md`**

In `README.md`, find:

```markdown
### `glane tags`
Lists your tag vocabulary with counts, most-used first — a map of what your veille
is actually about, and a way to spot drift (`k8s` vs `kubernetes`).

```sh
./glane tags
```

### `glane serve [--port N]`
```

Replace with:

```markdown
### `glane tags`
Lists your tag vocabulary with counts, most-used first — a map of what your veille
is actually about, and a way to spot drift (`k8s` vs `kubernetes`).

```sh
./glane tags
```

### `glane stats [-json]`
Prints a point-in-time snapshot of what's indexed: total items and the breakdown
per source, how many are enriched (article extracted) and summarized, how many
have embeddings, the distinct tag count, and the last sync time per live source.
Add `-json` for machine-readable output (same fields, JSON-encoded).

```sh
./glane stats
./glane stats -json
```

### `glane serve [--port N]`
```

- [ ] **Step 7: Update `completions/glane.fish`**

In `completions/glane.fish`, find:

```fish
set -l cmds import sync search serve enrich summarize update tags version
```

Replace with:

```fish
set -l cmds import sync search serve enrich summarize update tags stats version
```

Find:

```fish
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a tags      -d "list tags"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a version   -d "print version"
```

Replace with:

```fish
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a tags      -d "list tags"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a stats     -d "show indexing stats"
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a version   -d "print version"
```

Find:

```fish
complete -c glane -n "__fish_seen_subcommand_from summarize" -l limit -d "max items this run"
```

Replace with:

```fish
complete -c glane -n "__fish_seen_subcommand_from summarize" -l limit -d "max items this run"
complete -c glane -n "__fish_seen_subcommand_from stats"     -l json  -d "output as JSON"
```

- [ ] **Step 8: Commit**

```bash
git add main.go README.md completions/glane.fish
git commit -m "feat(cli): add glane stats command"
```

---

### Task 3: `/stats` web page

**Files:**
- Modify: `internal/web/web.go` (new `/stats` route)
- Create: `internal/web/templates/stats.html`
- Modify: `internal/web/templates/index.html` (nav link to `/stats`)
- Modify: `internal/web/web_test.go` (new tests)

**Interfaces:**
- Consumes: `store.Stats`, `func (s *store.Store) Stats() (store.Stats, error)` from
  Task 1; existing `tmpl *template.Template` (parsed via `ParseFS(assets,
  "templates/*.html")`), existing `funcs` template.FuncMap entry `"reltime"` (French
  relative time, unrelated to Task 2's English `relTime` in `main.go` — same name
  coincidence, different package, both intentional per the spec).
- Produces: nothing consumed by other tasks — this is the last task.

- [ ] **Step 1: Write the failing tests**

In `internal/web/web_test.go`, add (anywhere after the existing test functions,
before EOF):

```go
func TestStatsPageShowsCounts(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	seedTagged(t, s, "1", "alpha", []string{"rust"})

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div class="n">1</div>`) {
		t.Fatalf("expected a stat card showing 1: %s", body)
	}
	if !strings.Contains(body, "<td>bluesky</td>") {
		t.Fatalf("expected bluesky row in per-source table: %s", body)
	}
}

func TestIndexHasStatsLink(t *testing.T) {
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()

	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec.Body.String(), `href="/stats"`) {
		t.Fatalf("index missing nav link to /stats: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/web/ -run 'TestStatsPageShowsCounts|TestIndexHasStatsLink' -v`
Expected: FAIL — `TestStatsPageShowsCounts` gets a 404 (no `/stats` route yet) and
`TestIndexHasStatsLink` doesn't find `href="/stats"` in `index.html`.

- [ ] **Step 3: Add the `/stats` route**

In `internal/web/web.go`, find:

```go
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
```

Insert immediately before it:

```go
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
```

- [ ] **Step 4: Create `internal/web/templates/stats.html`**

```html
<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>glane · statistiques</title>
  <meta name="description" content="Statistiques sur votre veille technologique glanée : items importés, enrichis, résumés et vectorisés.">
  <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>🌾</text></svg>">
  <style>
    :root {
      --bg: #faf9f7;
      --surface: #ffffff;
      --surface-2: #f3f1ec;
      --border: #e7e2da;
      --border-strong: #d8d2c7;
      --ink: #1c1b18;
      --muted: #6c665d;
      --faint: #9a9389;
      --accent: #b45309;
      --accent-ink: #8a3f07;
      --accent-soft: #fbeddd;
      --ring: rgba(180, 83, 9, .32);
      --shadow: 0 1px 2px rgba(60, 50, 30, .05), 0 8px 24px -12px rgba(60, 50, 30, .16);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #141310;
        --surface: #1c1b16;
        --surface-2: #24221b;
        --border: #2d2b23;
        --border-strong: #3a372d;
        --ink: #ece9e2;
        --muted: #a49d92;
        --faint: #736d62;
        --accent: #e79f56;
        --accent-ink: #efb277;
        --accent-soft: #2a2015;
        --ring: rgba(231, 159, 86, .3);
        --shadow: 0 1px 2px rgba(0, 0, 0, .3), 0 10px 30px -14px rgba(0, 0, 0, .6);
      }
    }
    * { box-sizing: border-box; }
    html { scroll-behavior: smooth; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font-family: "Geist", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
      font-size: 15px;
      line-height: 1.5;
      -webkit-font-smoothing: antialiased;
      text-rendering: optimizeLegibility;
    }
    .wrap { max-width: 780px; margin: 0 auto; padding: 0 1.25rem; }
    header {
      padding: 3.25rem 0 1.5rem;
      display: flex; align-items: baseline; justify-content: space-between;
    }
    .brand { display: flex; align-items: baseline; gap: .6rem; }
    .brand h1 {
      font-size: 2rem; font-weight: 600; letter-spacing: -.03em;
      margin: 0; line-height: 1;
    }
    .brand .mark { font-size: 1.5rem; line-height: 1; transform: translateY(1px); }
    a.back { color: var(--muted); text-decoration: none; font-size: .9rem; }
    a.back:hover { color: var(--accent-ink); }
    main { padding-bottom: 5rem; }
    h2 {
      font-family: "Geist Mono", ui-monospace, "SF Mono", Menlo, monospace;
      font-size: .74rem; font-weight: 500; letter-spacing: .06em;
      text-transform: uppercase; color: var(--faint); margin: 1.6rem 0 .8rem;
    }
    .cards {
      display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
      gap: .8rem;
    }
    .card {
      background: var(--surface); border: 1px solid var(--border);
      border-radius: .8rem; padding: 1rem 1.1rem; box-shadow: var(--shadow);
    }
    .card .n {
      font-size: 1.6rem; font-weight: 600; letter-spacing: -.02em;
      font-variant-numeric: tabular-nums;
    }
    .card .label {
      font-family: "Geist Mono", ui-monospace, "SF Mono", Menlo, monospace;
      font-size: .72rem; font-weight: 500; letter-spacing: .06em;
      text-transform: uppercase; color: var(--faint); margin-top: .2rem;
    }
    table { width: 100%; border-collapse: collapse; }
    table td { padding: .4rem 0; border-bottom: 1px solid var(--border); }
    table td:last-child { text-align: right; font-variant-numeric: tabular-nums; color: var(--muted); }
  </style>
</head>
<body>
  <div class="wrap">
    <header>
      <div class="brand">
        <span class="mark" aria-hidden="true">🌾</span>
        <h1>glane</h1>
      </div>
      <a class="back" href="/">← Recherche</a>
    </header>

    <main>
      <div class="cards">
        <div class="card"><div class="n">{{.Total}}</div><div class="label">Total</div></div>
        <div class="card"><div class="n">{{.Enriched}}</div><div class="label">Enrichis</div></div>
        <div class="card"><div class="n">{{.Summarized}}</div><div class="label">Résumés</div></div>
        <div class="card"><div class="n">{{.Embedded}}</div><div class="label">Embeddings</div></div>
        <div class="card"><div class="n">{{.DistinctTags}}</div><div class="label">Tags</div></div>
      </div>

      <h2>Par source</h2>
      <table>
        {{range .BySource}}<tr><td>{{.Source}}</td><td>{{.Count}}</td></tr>{{end}}
      </table>

      {{if .LastSyncBySource}}
      <h2>Dernier sync</h2>
      <table>
        {{range .LastSyncBySource}}<tr><td>{{.Source}}</td><td>{{reltime .UpdatedAt}}</td></tr>{{end}}
      </table>
      {{end}}
    </main>
  </div>
</body>
</html>
```

- [ ] **Step 5: Add the nav link in `index.html`**

In `internal/web/templates/index.html`, find:

```html
    header { padding: 3.25rem 0 1.5rem; }
```

Replace with:

```html
    header {
      padding: 3.25rem 0 1.5rem;
      display: flex; align-items: baseline; justify-content: space-between;
    }
    a.stats-link { color: var(--muted); text-decoration: none; font-size: .9rem; }
    a.stats-link:hover { color: var(--accent-ink); }
```

Then find:

```html
    <header>
      <div class="brand">
        <span class="mark" aria-hidden="true">🌾</span>
        <h1>glane</h1>
        <p>votre veille, retrouvée d'un mot</p>
      </div>
    </header>
```

Replace with:

```html
    <header>
      <div class="brand">
        <span class="mark" aria-hidden="true">🌾</span>
        <h1>glane</h1>
        <p>votre veille, retrouvée d'un mot</p>
      </div>
      <a class="stats-link" href="/stats">Stats</a>
    </header>
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/web/ -run 'TestStatsPageShowsCounts|TestIndexHasStatsLink' -v`
Expected: PASS

- [ ] **Step 7: Run the full web package test suite (no regressions)**

Run: `mise exec -- go test ./internal/web/`
Expected: PASS (`ok  	github.com/jcgay/glane/internal/web	...`)

- [ ] **Step 8: Run the full repo test suite and `go vet`**

Run: `mise exec -- go build -o glane . && go vet ./... && go test ./...`
Expected: build succeeds, `go vet` silent, all packages `PASS`.

- [ ] **Step 9: Commit**

```bash
git add internal/web/web.go internal/web/web_test.go internal/web/templates/stats.html internal/web/templates/index.html
git commit -m "feat(web): add /stats page"
```
