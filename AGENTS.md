# CLAUDE.md

This file provides guidance to coding agents when working with code in this repository.

`glane` is a single Go binary + one SQLite file that indexes your saved tech-watch
posts (Twitter archive import, live GitHub/Mastodon/Bluesky sync) for full-text and
optional semantic search. The README is the source of truth for user-facing behavior,
commands, and env vars — read it before changing CLI surface or docs.

## Build & test

The Go toolchain version is pinned in `mise.toml`. Either activate mise in your shell
or prefix with `mise exec --`.

```sh
go build -o glane .          # self-contained binary (web UI assets are embedded)
go test ./...                # all tests
go test ./internal/search/   # one package
go test ./internal/search/ -run TestRRF   # one test
```

There is no separate lint step; keep `go vet ./...` clean.

## Architecture

Everything routes through one `*store.Store` (a `*sql.DB` over SQLite, pure-Go
`modernc.org/sqlite` driver — **no cgo**). `main.go` is a hand-rolled command
dispatcher (`switch os.Args[1]` → `cmd*` functions); each subcommand parses its own
`flag.FlagSet`. No CLI framework.

- **`internal/store`** — the only package that touches SQL. One `items` table with an
  `items_fts` FTS5 mirror kept in sync by INSERT/UPDATE/DELETE **triggers** (defined in
  the `schema` const in `store.go`) — do not write to `items_fts` directly. Vectors live
  in a separate `embeddings` table; per-source incremental cursors in `sync_state`. Items
  are deduplicated on `UNIQUE(source, source_id)`, which is what makes every import/sync
  safe to re-run.
- **`internal/search`** — `search.Hybrid` is the single entry point shared by CLI and web.
  It fuses FTS5 `bm25` results with brute-force cosine over stored vectors via Reciprocal
  Rank Fusion (`RRF`). The semantic layer **degrades silently to FTS-only** on any failure
  (nil embed client, embed error, no vectors) — never surfaces an error. Preserve that.
- **Connectors** (`twitter`, `github`, `mastodon`, `bluesky`) — each turns a source into
  `store.Item`s; the live ones (github/mastodon/bluesky) advance a `sync_state` cursor only
  after a fully successful run, so an interrupted sync just re-fetches next time.
- **`internal/enrich`** — fetches each item's primary link and extracts article body
  (`go-shiori/go-readability`); also generates embeddings when an embed endpoint is set.
- **`internal/summarize`** — optional single chat-completion per article → summary + tags.
  Additive: never overwrites indexed article text, leaves embeddings untouched.
- **`internal/embed`** — thin OpenAI-compatible embeddings client; nil when `GLANE_EMBED_URL` unset.
- **`internal/web`** — `serve` UI; templates + htmx are `go:embed`ed, so the binary ships standalone.

## Conventions

- `sync`/`enrich`/`summarize` print live **progress to stderr**; the final result goes to
  **stdout** so piping stays clean. Keep this split.
- External LLM/embedding endpoints are entirely optional and driven by env vars — every
  feature must still work offline with no model configured.
- Update `README.md` **and** `completions/glane.fish` whenever CLI surface, flags, or
  env vars change (see auto-memory).
