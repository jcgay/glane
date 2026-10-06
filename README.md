# glane

Search your tech-watch goldmine — the posts you liked, reposted, and bookmarked
across networks — from one place, by keyword or by meaning.

`glane` is a single Go binary over one SQLite file. It imports your saved posts,
indexes them (and the text of the articles they link to) for full-text search,
and — when you point it at an embeddings endpoint — adds semantic search on top.
Everything works offline with no model; the semantic layer is an optional bonus.

It imports a **Twitter/X archive** and syncs live sources — **GitHub stars**,
**Mastodon** (favourites, bookmarks, your posts + boosts), and **Bluesky**
(likes, saved posts, your posts + reposts) — into one index.
An optional LLM step summarizes and tags each saved article, so you can recognize
a forgotten bookmark at a glance and browse your veille by topic.

## Install

### On your computer

```sh
brew install jcgay/jcgay/glane
```

That's all full-text search needs. Everything else is optional, and only
for the feature it names:

| To… | Install | See |
|-----|---------|-----|
| sync GitHub, Mastodon, Bluesky | nothing, just a token per source | [Quick start](#quick-start) |
| search by meaning | [Ollama](https://ollama.com) (`brew install ollama`) or any OpenAI-compatible embeddings API | [Semantic search](docs/semantic-search.md) |
| summarize and tag articles | the same, with a chat model | [`glane summarize`](docs/commands.md#glane-summarize---limit-n) |
| share the index with other machines or a phone | [Syncthing](https://syncthing.net) (`brew install syncthing`) | [Sharing across machines](docs/sharing.md) |
| keep the index fresh on a schedule | nothing, `glane update` runs sync, enrich and summarize | [Scheduling](#scheduling) |

### On an Android phone

- An arm64 phone on Android 11 or newer.
- [Syncthing-Fork](https://f-droid.org/packages/com.github.catfriend1.syncthingfork/)
  from F-Droid, to receive `glane.db` from your computer.
- The glane APK: `glane_<version>_android_arm64.apk`, attached to each
  [release](https://github.com/jcgay/glane/releases). To be told about new
  ones and install them from the phone, add the repository to
  [Obtainium](https://github.com/ImranR98/Obtainium).

Then follow [Android](docs/android.md) to share the database and set the app up.

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

To pull in your live sources too:

```sh
export GLANE_GITHUB_TOKEN=…        # GitHub stars
export MASTODON_INSTANCE_URL=https://mastodon.social MASTODON_ACCESS_TOKEN=…
export BLUESKY_HANDLE=you.bsky.social BLUESKY_APP_PASSWORD=…
./glane sync all                   # syncs every configured source (incremental)
```

To enrich links and add LLM summaries + tags:

```sh
./glane enrich                     # fetch linked articles, extract their text
export GLANE_SUMMARY_URL=http://localhost:11434/v1 GLANE_SUMMARY_MODEL=gemma3
./glane summarize                  # summarize + tag each enriched article
./glane tags                       # see your topic vocabulary
./glane search --tag kubernetes    # browse everything tagged kubernetes
```

## Commands

| Command | What it does |
|---------|--------------|
| `glane import twitter <archive-dir>` | Import a Twitter/X data export |
| `glane sync github\|mastodon\|bluesky` | Sync one live source, incrementally |
| `glane sync all` | Sync every configured source |
| `glane update` | `sync all`, then `enrich`, then `summarize`: the command to schedule |
| `glane search [query] [flags]` | Search, or list with no query (`--source`, `--tag`, `--since`, `--limit`) |
| `glane enrich [--limit N] [--force]` | Fetch each item's link and index the article text |
| `glane summarize [--limit N]` | Summarize and tag enriched articles with an LLM |
| `glane tags` | List your tags with counts |
| `glane stats [-json]` | Show what's indexed |
| `glane serve [--port N] [--read-only]` | Serve the keyboard-first web UI |

Every flag, example and edge case: [docs/commands.md](docs/commands.md).

## Semantic search

Semantic search finds a post by meaning even when you've forgotten its exact
words. Set `GLANE_EMBED_URL` and `GLANE_EMBED_MODEL` to an OpenAI-compatible
embeddings endpoint (Ollama locally, or a remote API) and run `enrich`;
unset, `glane` stays full-text only. Setup and model choice:
[docs/semantic-search.md](docs/semantic-search.md).

## Data location

State lives in one SQLite file. By default:

```
~/.local/share/glane/glane.db
```

Override it with `GLANE_DB=/path/to/glane.db`. Delete the file to start over.

## Sharing across machines

`glane` is one SQLite file: point `GLANE_DB` at a folder kept in sync by
Syncthing, Dropbox or iCloud Drive on each machine, one machine at a time.
The rules that keep the file consistent: [docs/sharing.md](docs/sharing.md).

## Android

An Android app searches your index offline, read-only, over the copy of
`glane.db` that Syncthing keeps on the phone. Setup:
[docs/android.md](docs/android.md).

## Environment variables

| Variable | Used by | Meaning |
|----------|---------|---------|
| `GLANE_DB` | all | SQLite file path (default `~/.local/share/glane/glane.db`) |
| `GLANE_GITHUB_TOKEN` | `sync github` | GitHub token (read-only is enough); falls back to `GITHUB_TOKEN` |
| `MASTODON_INSTANCE_URL` | `sync mastodon` | Instance base URL, e.g. `https://mastodon.social` |
| `MASTODON_ACCESS_TOKEN` | `sync mastodon` | Access token (`read:favourites` + `read:bookmarks`) |
| `BLUESKY_HANDLE` | `sync bluesky` | Your handle, e.g. `you.bsky.social` |
| `BLUESKY_APP_PASSWORD` | `sync bluesky` | An app password (not your main password) |
| `GLANE_EMBED_URL` | `search`, `enrich` | OpenAI-compatible embeddings base URL; unset → semantic disabled |
| `GLANE_EMBED_MODEL` | `search`, `enrich` | Embedding model name |
| `GLANE_EMBED_KEY` | `search`, `enrich` | Embeddings API key; omit for local endpoints |
| `GLANE_SUMMARY_URL` | `summarize` | OpenAI-compatible chat base URL; unset → `summarize` errors |
| `GLANE_SUMMARY_MODEL` | `summarize` | Chat model name (e.g. `gemma3`) |
| `GLANE_SUMMARY_KEY` | `summarize` | Chat API key; omit for local endpoints |
| `GLANE_SUMMARY_TIMEOUT` | `summarize` | Per-request timeout in seconds (default 180) |
| `GLANE_SERVE_TOKEN` | `serve --read-only` | Required as the `glane_token` cookie on every request; unset → no check |

> The Mastodon/Bluesky variables intentionally match
> [social-timeline](https://github.com/jcgay/social-timeline), so the same
> credentials drive both tools. Note glane reads your favourites + bookmarks, so
> `MASTODON_ACCESS_TOKEN` needs `read:favourites` + `read:bookmarks` (social-timeline
> only needs `read:statuses`).

## Scheduling

`glane update` is meant to run on a timer. Ready-to-copy recipes — launchd on
macOS, cron on Linux, and the bare-environment pitfalls both share — live in
[docs/scheduling.md](docs/scheduling.md).

## Use from Claude Code

This repo doubles as a [Claude Code plugin
marketplace](https://code.claude.com/docs/en/plugin-marketplaces), so an agent
can search your veille for you — grounding its answers in the sources you've
already curated instead of its training data.

```
/plugin marketplace add jcgay/glane
/plugin install glane@glane
```

The plugin ships one **read-only** skill: it only runs `glane search` and
`glane tags`, and a bundled `PreToolUse` hook blocks any mutating or
long-running `glane` command (`sync`, `enrich`, `summarize`, `update`,
`import`, `serve`) so read-only holds even if the agent tries. It needs the `glane`
binary on `PATH` and a populated database (set `GLANE_DB` if you don't use the
default location). Details in [`plugins/glane/`](plugins/glane/).

## Development

Building the Go binary and the Android app, and how the pieces fit
together: [docs/development.md](docs/development.md).

## Roadmap

Done: the searchable core (Twitter import, full-text + semantic search, web UI,
link enrichment), live connectors for GitHub stars, Mastodon, and Bluesky with
`sync all`, the optional LLM summaries + tags, and `glane update` with a
documented scheduled entry (see [Scheduling](#scheduling)). Ideas for later:

- Tag normalization/aliasing if the free-tag vocabulary drifts (inspect with `glane tags`)
- A `--quiet` flag to silence progress output

Design and implementation notes live in `docs/superpowers/`.
