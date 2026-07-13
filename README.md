# glane

Search your tech-watch goldmine — the posts you liked, reposted, and bookmarked
across networks — from one place, by keyword or by meaning.

`glane` is a single Go binary over one SQLite file. It imports your saved posts,
indexes them (and the text of the articles they link to) for full-text search,
and — when you point it at an embeddings endpoint — adds semantic search on top.
Everything works offline with no model; the semantic layer is an optional bonus.

It imports a **Twitter/X archive** and syncs your **GitHub stars**; live
connectors for Bluesky and Mastodon are planned (see [Roadmap](#roadmap)).

## Requirements

- [mise](https://mise.jdx.dev/) (manages the Go toolchain; version pinned in `mise.toml`)
- Or Go 1.25+ directly if you prefer not to use mise

```sh
mise install          # installs the pinned Go
```

## Build

```sh
mise exec -- go build -o glane .
# or, with mise activated in your shell:  go build -o glane .
```

This produces a self-contained `glane` binary (the web UI assets are embedded).

## Quick start

```sh
# 1. Import your Twitter archive (the folder containing data/like.js, data/tweets.js)
./glane import twitter ./twitter

# 2. Search from the terminal
./glane search kubernetes networking --limit 5

# 3. …or browse in the local web UI
./glane serve            # then open http://127.0.0.1:8080
```

Full-text search works immediately — no network, no model.

To pull in a live source too:

```sh
export GITHUB_TOKEN=…     # a read-only token; often already set (gh CLI, CI)
./glane sync github       # imports your stars (incremental on re-run)
```

## Commands

### `glane import twitter <archive-dir>`
Imports a Twitter/X data export. Point it at the top-level export folder (the one
containing `data/like.js` and `data/tweets.js`). Likes, your own tweets, and
reposts are all indexed. Re-running is safe — items are deduplicated on their
source id.

### `glane sync github`
Syncs your GitHub stars into the index. Requires a token in the `GITHUB_TOKEN`
environment variable (a read-only classic token is enough for public stars; the
conventional name means an already-exported token from the `gh` CLI or CI just
works).

```sh
export GITHUB_TOKEN=…
./glane sync github
```

- First run backfills every star; later runs are **incremental** — a persistent
  per-source cursor means only newly-starred repos are fetched.
- Safe to re-run: the cursor only advances after a fully successful sync, so an
  interrupted run just re-fetches next time (imports are deduplicated).
- Each star is stored with `--source github` and its `starred_at` date (so
  `--since` works). Run `enrich` afterwards to pull in each repo's page content.

### `glane search <query> [flags]`
Searches the index. **The query comes first** (multiple words are fine unquoted);
flags come after.

| Flag | Default | Meaning |
|------|---------|---------|
| `--source` | all | Restrict to one source (`twitter`, `bluesky`, `mastodon`, `github`) |
| `--since` | — | Only items on/after a date: `YYYY` or `YYYY-MM-DD` |
| `--limit` | 20 | Max results |

```sh
./glane search cold start lambda --source twitter --limit 10
./glane search "provisioned concurrency" --since 2022
```

If an embeddings endpoint is configured (see [Semantic search](#semantic-search)),
results blend full-text and semantic rankings automatically. If not, it's
full-text only — same command, no error.

### `glane enrich [--limit N]`
Most saved posts are just a link; the value is the article behind it. `enrich`
fetches each item's primary link, extracts the article body, and adds it to the
search index — so you can find a post by the content of the page it pointed to,
not just its 100-character text.

```sh
./glane enrich --limit 100
```

- Resumable: only un-fetched items are processed, so you can run it in batches.
- Dead links (common for old `t.co` URLs) are marked failed; the post stays
  searchable by its own text.
- If an embeddings endpoint is configured, `enrich` also generates and stores a
  vector for each enriched item.

### `glane serve [--port N]`
Serves the local web UI (default `http://127.0.0.1:8080`) — a single page with
search-as-you-type and a source filter. Local-only; no auth.

## Semantic search

Semantic search lets you find a post by meaning even when you've forgotten its
exact words. It's entirely optional and driven by three environment variables. If
`GLANE_EMBED_URL` is unset, `glane` stays full-text only.

| Variable | Meaning |
|----------|---------|
| `GLANE_EMBED_URL` | Base URL of an **OpenAI-compatible** embeddings API. Unset → semantic disabled. |
| `GLANE_EMBED_MODEL` | Embedding model name |
| `GLANE_EMBED_KEY` | API key (sent as `Authorization: Bearer …`); omit for local endpoints |

`glane` calls `POST {GLANE_EMBED_URL}/embeddings` with `{ "model": …, "input": [...] }`.

### Local, with Ollama

Run a model on your machine (e.g. an M2) and point `glane` at it — free, private,
offline:

```sh
export GLANE_EMBED_URL=http://localhost:11434/v1
export GLANE_EMBED_MODEL=nomic-embed-text

./glane enrich          # generates embeddings while extracting articles
./glane search "how to reduce container image size"
```

### Remote API

```sh
export GLANE_EMBED_URL=https://api.openai.com/v1
export GLANE_EMBED_MODEL=text-embedding-3-small
export GLANE_EMBED_KEY=sk-…
```

Switching between local and remote is just changing these variables.

> Vectors are generated during `enrich`. After changing the embedding model,
> re-run `enrich` so the stored vectors match your query model.

## Data location

State lives in one SQLite file. By default:

```
~/.local/share/glane/glane.db
```

Override it with `GLANE_DB=/path/to/glane.db`. Delete the file to start over.

## Environment variables

| Variable | Used by | Meaning |
|----------|---------|---------|
| `GLANE_DB` | all | SQLite file path (default `~/.local/share/glane/glane.db`) |
| `GITHUB_TOKEN` | `sync github` | GitHub token (read-only is enough) |
| `GLANE_EMBED_URL` | `search`, `enrich` | OpenAI-compatible embeddings base URL; unset → semantic disabled |
| `GLANE_EMBED_MODEL` | `search`, `enrich` | Embedding model name |
| `GLANE_EMBED_KEY` | `search`, `enrich` | Embeddings API key; omit for local endpoints |

## How it works

- **Storage** — SQLite with an FTS5 mirror kept in sync by triggers, plus a table
  of embedding vectors. Pure-Go driver (`modernc.org/sqlite`), no cgo.
- **Search** — full-text via FTS5 `bm25`; semantic via brute-force cosine over
  stored vectors; the two are fused with Reciprocal Rank Fusion when both are
  available. The CLI and the web UI share one `search.Hybrid` entry point.
- **Enrichment** — article extraction via `go-shiori/go-readability`.

## Roadmap

Done: the searchable core (Twitter import, full-text + semantic search, web UI,
link enrichment) and the GitHub stars connector with its reusable sync
framework. Planned next:

- Live sync connectors for Bluesky and Mastodon (reusing the sync framework)
- `sync all` once a second connector lands
- Optional LLM article summaries
- A documented cron/launchd entry for scheduled syncing

Design and implementation notes live in `docs/superpowers/`.
