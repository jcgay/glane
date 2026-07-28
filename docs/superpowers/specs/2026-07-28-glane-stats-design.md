# glane stats — design

## Purpose

Give users a quick overview of what's in their glane database: how much has been
imported/synced, how much has been enriched (article extraction), summarized/tagged,
and embedded — plus per-source sync freshness. Available both as a CLI command
(`glane stats`) and a web UI page (`/stats`), sharing the same underlying data.

## Data model

No schema changes. All numbers are derived from existing tables:
`items`, `embeddings`, `item_tags`, `sync_state`.

### `internal/store/stats.go`

```go
type SourceCount struct {
    Source string
    Count  int
}

type SourceSync struct {
    Source    string
    UpdatedAt int64 // unix seconds, 0 = never synced
}

type Stats struct {
    Total          int
    BySource       []SourceCount // ordered by count desc, then source asc
    Enriched       int           // items with fetch_status = 'ok'
    Summarized     int           // items with article_summary != ''
    Embedded       int           // distinct item_id in embeddings
    DistinctTags   int           // distinct tag in item_tags
    LastSyncBySource []SourceSync // one row per source present in sync_state
}

func (s *Store) Stats() (Stats, error)
```

Implementation notes:
- `BySource`: `SELECT source, COUNT(*) FROM items GROUP BY source ORDER BY COUNT(*) DESC, source`.
- `Enriched`: `SELECT COUNT(*) FROM items WHERE fetch_status = 'ok'`. This is the
  existing success marker written by `internal/enrich` (see `store/enrich.go`
  `SaveEnrichment`); it intentionally excludes `'error'` and `''`/pending rows.
- `Summarized`: `SELECT COUNT(*) FROM items WHERE article_summary != ''`.
- `Embedded`: `SELECT COUNT(DISTINCT item_id) FROM embeddings`.
- `DistinctTags`: `SELECT COUNT(DISTINCT tag) FROM item_tags`.
- `LastSyncBySource`: `SELECT source, updated_at FROM sync_state ORDER BY source`.
- All queries run against the existing `*sql.DB`; no new indexes needed at this scale
  (single-user local SQLite file).
- Follows the same degrade-silently-on-error philosophy as `TagCounts`/`AttachTags`
  only at the call site (web handler), not inside `Stats()` itself: `Stats()` returns
  a real error like other store methods (`SearchFTS`, `TagCounts`), and callers decide
  how to handle it (CLI: `fatal`; web: log + best-effort empty state).

## CLI command

### `main.go`

- Add `"stats"` to the usage string and the `switch os.Args[1]` dispatch, calling
  `cmdStats(s, os.Args[2:])`.
- `cmdStats` parses a small `flag.FlagSet` with one flag: `-json` (bool, default false).
- Default (text) output — aligned, stderr/stdout split not applicable here since this
  is a one-shot summary, not a progress-emitting command; everything goes to stdout:

```
Total          1234 items
  twitter       800
  github        300
  mastodon      100
  bluesky        34
Enrichis        950 / 1234
Résumés         600 / 1234
Embeddings      950
Tags            42 distincts
Dernier sync
  github        il y a 2h
  mastodon      il y a 1j
  bluesky       jamais
```
  - Sources with no `sync_state` row (never synced, e.g. `twitter` which is
    import-only) are omitted from "Dernier sync" — only sources actually present
    in `sync_state` are listed. (`twitter` never appears there since it's a one-shot
    archive import, not a live sync connector.)
  - Relative time formatting reuses the same logic as the web `reltime` template func
    (a small shared implementation duplicated as a plain Go function since `reltime`
    currently lives inside `internal/web`'s template.FuncMap — see below).

- `-json` output: `json.MarshalIndent` of the `store.Stats` struct directly (field
  names as-is via default Go JSON tags — no need for custom tags since this is a
  read-only reporting struct, not a wire API contract).

### Shared relative-time helper

`reltime` currently lives as an unexported closure inside `internal/web/web.go`'s
`template.FuncMap`. Rather than introduce a dependency from `main` on `internal/web`
(or a new shared package for one helper), `main.go` gets its own small private
`relTime(ts int64) string` function — a straight copy of the same few lines already
in `internal/web/web.go`. This ~10-line duplication is consistent with the codebase's
existing preference for small, isolated packages over premature sharing. If a third
consumer needs it later, that's the trigger to extract a shared helper.

## Web UI

### Route

`internal/web/web.go`: add `mux.HandleFunc("/stats", ...)` calling `s.Stats()`,
logging+continuing on error (renders the page with zero values rather than a hard
500, consistent with the "degrade silently" philosophy used elsewhere for optional
data), then executing a new `stats.html` template.

### Template (`internal/web/templates/stats.html`)

Same visual language as `index.html` (same CSS variables, reuses the existing
`<style>` custom properties by living in the same template set so `template.FuncMap`
and embedded CSS custom properties are available). Layout: a grid of small stat
cards (Total, Enrichis, Résumés, Embeddings, Tags) above a "Par source" breakdown
table and a "Dernier sync" list. No htmx interactivity needed — it's a static
snapshot page, plain server-rendered HTML.

### Navigation

Add a simple link "Stats" in the `<header>` of `index.html` (next to the `glane`
brand), pointing to `/stats`. Add a symmetric "← Recherche" link back to `/` at the
top of `stats.html`.

## Testing

- `internal/store/stats_test.go`: seed a handful of items across sources with mixed
  `fetch_status`/`article_summary`/embeddings/tags/sync_state rows, assert `Stats()`
  returns the expected aggregates. Cover the zero-data case (empty DB → all zeros,
  no error).
- `internal/web/web_test.go`: add a case hitting `GET /stats` on a seeded store,
  assert 200 and that key numbers appear in the body.
- No new test file for `main.go`'s `cmdStats` (existing convention: `main.go`'s
  `cmd*` functions aren't unit-tested directly; they're thin wrappers over
  `store`/`internal/*` which already have their own tests). Manual smoke check
  (`go build && ./glane stats` / `./glane stats -json`) during implementation is
  sufficient, matching how other `cmd*` functions in this codebase are validated.

## Out of scope

- No historical/trend data (e.g., items added per day) — this is a point-in-time
  snapshot only.
- No new CLI flags beyond `-json` (no `-source` filter, etc.) — YAGNI until requested.
- No changes to `sync_state` schema to track per-source item counts incrementally —
  computed on read, which is cheap at expected data volumes (single-user archive,
  thousands not millions of rows).
