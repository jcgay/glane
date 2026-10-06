# Commands

## `glane import twitter <archive-dir>`
Imports a Twitter/X data export. Point it at the top-level export folder (the one
containing `data/like.js` and `data/tweets.js`). Likes, your own tweets, and
reposts are all indexed. Re-running is safe — items are deduplicated on their
source id.

## `glane sync github`
Syncs your GitHub stars into the index. Requires a token in the
`GLANE_GITHUB_TOKEN` environment variable (a read-only classic token is enough
for public stars). `GITHUB_TOKEN` is used as a fallback, so a token already
exported in CI just works; prefer the prefixed name on your machine, because
the `gh` CLI uses an exported `GITHUB_TOKEN` in place of its own login.

```sh
export GLANE_GITHUB_TOKEN=…
./glane sync github
```

- First run backfills every star; later runs are **incremental** — a persistent
  per-source cursor means only newly-starred repos are fetched.
- Safe to re-run: the cursor only advances after a fully successful sync, so an
  interrupted run just re-fetches next time (imports are deduplicated).
- Each star is stored with `--source github` and its `starred_at` date (so
  `--since` works). Run `enrich` afterwards to pull in each repo's page content.

## `glane sync mastodon`
Syncs four streams from Mastodon: **favourites** (stored as likes), **bookmarks**,
your **own posts**, and your **boosts** (stored as reposts, mapped to the original
post). Requires `MASTODON_INSTANCE_URL` (your instance base, e.g.
`https://mastodon.social`) and `MASTODON_ACCESS_TOKEN` — a token scoped for
`read:favourites` + `read:bookmarks` + `read:statuses` (or a broad `read`).

```sh
export MASTODON_INSTANCE_URL=https://mastodon.social
export MASTODON_ACCESS_TOKEN=…
./glane sync mastodon
```

Each stream is incremental (its own cursor). Post text is HTML-stripped, keeping
the linked URL so `enrich` can fetch the shared article. Your replies are excluded.

## `glane sync bluesky`
Syncs four streams from Bluesky: posts you've **liked**, posts you've **saved**
(bookmarks), your **own posts**, and your **reposts** (stored as reposts, mapped
to the original post). Requires `BLUESKY_HANDLE` (e.g. `you.bsky.social`) and
`BLUESKY_APP_PASSWORD` — create an **app password** in Bluesky settings, don't
use your main password.

```sh
export BLUESKY_HANDLE=you.bsky.social
export BLUESKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
./glane sync bluesky
```

Each stream is incremental. Your replies are excluded. A post you've touched
several ways (e.g. liked *and* reposted) is stored once, labelled by the strongest
relationship (posts/reposts > bookmark > like).

## `glane sync all`
Runs every connector whose config is present, skipping the rest (reported, not
errored). Ideal for a scheduled job: it keeps going if one source fails, and
exits non-zero if any configured connector errored — so cron/launchd can alert.

```sh
./glane sync all
```

## `glane update`
Runs the whole pipeline in one shot — `sync all` → `enrich` → `summarize` —
draining the backlog. Skips unconfigured pieces and exits non-zero if any phase
fails. This is the command to schedule.

```sh
./glane update
```

## `glane search [query] [flags]`
Searches the index. **The query comes first** (multiple words are fine unquoted);
flags come after. A query word starting with `-` would read as a flag: put the
flags first and the query after `--` (`glane search --tag go -- -foo`). Words match whole tokens, except the last word, which matches
as a prefix (type-ahead): `useTa` finds `useTabs`.

With **no query**, it lists instead of searching: `--tag` browses that tag, and
no query at all lists your newest items — pair it with `--since` to review
everything that landed while you were away.

| Flag | Default | Meaning |
|------|---------|---------|
| `--source` | all | Restrict to one source (`twitter`, `bluesky`, `mastodon`, `github`) |
| `--tag` | — | Restrict to a tag (see `glane summarize`); with no query, browses that tag |
| `--since` | — | Only items on/after a date: `YYYY` or `YYYY-MM-DD`, read as your local midnight, or a window back from today: `7d`, `2w`, `3m`, `1y` |
| `--limit` | 20 | Max results |

`--since` filters on the item's own date, which is when *you* starred it for
GitHub but when the post was *published* for twitter/mastodon/bluesky — a
five-year-old article you liked yesterday sorts by its own age, not by yours.

Results lead with the LLM summary (when present) and show the item's tags.
Matched terms are highlighted wherever they appear — title, summary, or text —
and when a match falls only in the hidden article body, that passage is shown as
an extra highlighted excerpt so you can see why the result came up.

```sh
./glane search cold start lambda --source twitter --limit 10
./glane search "provisioned concurrency" --since 2022
./glane search --tag rust           # browse everything tagged rust, newest first
./glane search --since 2026-07-20 --limit 100   # review everything since a date
./glane search --since 2026-07-20 --source github --limit 100   # …just the repos
./glane search --since 7d          # what landed this past week
```

If an embeddings endpoint is configured (see [Semantic search](semantic-search.md)),
results blend full-text and semantic rankings automatically. If not, it's
full-text only — same command, no error.

## `glane enrich [--limit N] [--force]`
Most saved posts are just a link; the value is the article behind it. `enrich`
fetches each item's primary link, extracts the article body, and adds it to the
search index — so you can find a post by the content of the page it pointed to,
not just its 100-character text.

```sh
./glane enrich --limit 100
```

- Resumable: only un-fetched items are processed, so you can run it in batches.
- The stored link is the **final URL after redirects** with tracking params
  (`utm_*`, `fbclid`, …) stripped — so `t.co` and other shorteners are resolved.
- Dead links (common for old `t.co` URLs) are marked failed; the post stays
  searchable by its own text.
- If an embeddings endpoint is configured, `enrich` also generates and stores a
  vector for each enriched item.
- `--force` re-enriches already-fetched items (re-resolve links, backfill
  embeddings for items indexed before an embed endpoint was set). It resets
  every fetched item, then processes up to `--limit`; run repeatedly (or with a
  high `--limit`) to drain the backlog.

## `glane summarize [--limit N]`
Optional LLM step. For each enriched article without one yet, a single
chat-completions call produces a **readable summary** and **3–6 free topic tags**.
Requires `GLANE_SUMMARY_URL` (an OpenAI-compatible chat endpoint) + `GLANE_SUMMARY_MODEL`
(+ optional `GLANE_SUMMARY_KEY`); unset → the command tells you to set it.

```sh
export GLANE_SUMMARY_URL=http://localhost:11434/v1   # e.g. Ollama on your M2
export GLANE_SUMMARY_MODEL=gemma3
./glane summarize --limit 200
```

- Resumable: only un-summarized enriched items are processed.
- Fail-soft: an item the model can't summarize is logged and skipped, retried next run.
- A busy server (`503`/`429`) is retried a few times with backoff. Each request
  waits up to `GLANE_SUMMARY_TIMEOUT` seconds (default 180) — raise it if a slow
  local model gets cut off mid-generation (a premature cutoff can wedge a
  single-slot server into refusing every following request).
- The summary is searchable (full-text) and becomes the result snippet; tags feed
  `--tag` and `glane tags`. Embeddings are left untouched (a summary vector isn't
  reliably better than the existing one).

## `glane tags`
Lists your tag vocabulary with counts, most-used first — a map of what your veille
is actually about, and a way to spot drift (`k8s` vs `kubernetes`).

```sh
./glane tags
```

## `glane stats [-json]`
Prints a point-in-time snapshot of what's indexed: total items and the breakdown
per source, how many are enriched (article extracted) and summarized, how many
have embeddings, the distinct tag count, and the last sync time per live source.
Add `-json` for machine-readable output (same fields, JSON-encoded).

```sh
./glane stats
./glane stats -json
```

## `glane serve [--port N] [--read-only]`
Serves the local web UI (default `http://127.0.0.1:8080`), a search console
built for the keyboard. The page opens on your newest items and searches as you
type.

- **Filters are words in the query**, the same as the CLI flags:
  `cold start source:twitter tag:aws since:30d`; quote a value with spaces,
  `tag:"software engineering"`. The filter rail (sources,
  `7d`/`30d`/`1y`, tags) only writes those words into the box, and each listing
  shows the matching `glane search …` command, ready to copy. A filter it
  can't apply (`since:abc`, an unknown source, a second `tag:`) is named in
  the status line rather than silently dropped.
- **The query is in the URL** (`/?q=…`): reload, bookmark or share a search;
  Back undoes the last filter or Enter.
- **Keyboard**: `/` search, `esc` leave the box (the query stays), `j`/`k`
  move through results, `o` open the link, `O` the original post, `y` copy the
  URL, `Y` a markdown link, `1`–`4` pick a source (`0` all), `[` hide the
  rail, `?` list all of this.
- **Touch screens**: a tap on a result shows its whole summary; tap its title
  again to open the link, or tap the `↗` link to go there at once.
- **Index health** in the rail: how much is enriched, summarized and embedded,
  with the command that fills each gap. The status bar shows the item count,
  whether semantic search is on (and its model), and the last sync per source.
- **`--read-only`** never writes the database: no schema migration, no
  journal file, and a missing file is an error rather than a new empty
  database. When a sync tool replaces the file, the next request picks up
  the new copy without a restart. This is how the Android app serves the
  copy Syncthing brings to the phone.

Local-only; no auth.

The UI speaks **English and French**, picked from your browser's
`Accept-Language` (English when it asks for neither). Nothing to configure.
