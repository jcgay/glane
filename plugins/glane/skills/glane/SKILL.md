---
name: glane
description: Use when you need to know what the user has already read, liked, bookmarked, reposted, or starred about a technical topic — grounding an answer in their own sources, researching something they actively follow, digging up a half-remembered saved article, or checking their prior art before recommending a tool or library. Searches the user's personal tech-watch ("veille") through the local `glane` CLI. Read-only — runs `search` and `tags`, never a command that changes data.
---

# Searching the user's tech-watch with glane

`glane` is the user's personal tech-watch index: the posts they liked, reposted,
bookmarked, and starred across Twitter/X, Mastodon, Bluesky, and GitHub — plus
the full text of the articles those posts linked to, and optional LLM summaries
and tags. When a question touches a topic the user tracks, their curated corpus
is sharper grounding than your training data. Search it first, then cite what
you find.

## When this helps

- "What have I saved about X?" / "find that article on X I bookmarked"
- Before recommending a library, framework, or approach — see what they already vetted
- Researching a topic they actively follow (their sources are pre-filtered by their taste)

## Preflight

Needs the `glane` binary on PATH and a populated database. If `glane search
anything --limit 1` errors (binary missing) or every varied query returns
`(0 results)` (empty DB), glane isn't set up — say so plainly. Do **not** try to
`import` or `sync` to populate it yourself; that's the user's job.

## Use it (read-only)

Run only `search` and `tags`. **The query comes first, flags after** — multiple
words need no quotes:

```sh
glane search kubernetes networking --limit 10
glane search provisioned concurrency lambda --source twitter --since 2022
glane search --tag rust            # browse everything tagged rust (no query needed)
glane tags                         # the user's topic vocabulary, most-used first
```

| Flag | Meaning |
|------|---------|
| `--source` | one of `twitter`, `mastodon`, `bluesky`, `github` |
| `--tag` | restrict to a tag (or browse it with no query) |
| `--since` | `YYYY` or `YYYY-MM-DD` |
| `--limit` | max results (default 20) |

Run `glane search --help` for the authoritative flag list rather than trusting this table if a command errors.

## Reading the output

Plain text, one entry per result, newest/most-relevant first:

```
  1. [twitter/like]  <one-line snippet: LLM summary if present, else post text>
     https://example.com/article  #tag1 #tag2

(3 results)
```

- The snippet is truncated (~160 chars). The **URL is the real source** — follow
  it (e.g. WebFetch) when you need the article's full content, don't answer from
  the snippet alone.
- `#tags` after the URL feed `--tag`. The final `(N results)` line is the count.
- Results already blend full-text and (if the user configured embeddings)
  semantic ranking — one query does both; you don't pick a mode.

## Common mistakes

- **Running a mutating command.** Never run `sync`, `enrich`, `summarize`,
  `update`, or `import`. They write to the user's data, need credentials, hit
  the network, and are slow — they are the user's scheduled maintenance, not a
  step in answering a question.
- **Expecting JSON.** There is no `--json` output; parse the text format above.
- **Over-quoting.** `glane search cold start lambda` is correct. Quote only to
  keep a phrase from being read as flags.
- **Trusting the snippet as the whole answer.** It's a lead; open the URL for
  substance.
