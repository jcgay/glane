# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

A developer who keeps a tech watch ("veille") by liking, reposting, bookmarking
and starring things across networks: Twitter/X, GitHub, Mastodon, Bluesky. The
maintainer is the first user and their workflow decides; the tool is published
(Homebrew tap, GitHub releases, Android APK) so other developers with the same
habit can use it.

They work at a computer, next to a terminal and an editor, and sometimes on an
Android phone away from it.

## Product Purpose

glane brings every saved post, and the text of the articles they link to, into
one local index, so a saved item is never lost to a network's own search.

It serves two jobs, equally:

- **Retrieval:** the user half-remembers something saved weeks or years ago and
  needs it back fast, by keyword or by meaning.
- **Rediscovery:** the user browses their watch by topic, source and period,
  with LLM summaries and tags making forgotten bookmarks recognizable at a
  glance.

Success: the item comes back in a few keystrokes, and browsing the index
resurfaces things worth reading again.

## Positioning

One Go binary over one SQLite file, owned by the user. Full-text search works
offline with no model; semantic search, summaries and tags are optional layers
on any OpenAI-compatible endpoint (local Ollama or remote) and degrade silently
when absent. Sources are merged into one index rather than searched per network.

## Operating Context

- CLI: `import`, `sync`, `enrich`, `summarize`, `update` (scheduled), `search`,
  `tags`, `stats`. Progress on stderr, results on stdout, so output pipes.
- Web UI (`glane serve`): a keyboard-first console over the same index, with
  facets (source, tag, since) and a stats page that points at commands to fill
  gaps.
- Desktop: `glane app` opens that UI in a native window with its own server.
- Android: a wrapper around `glane serve --read-only` over the copy of
  `glane.db` that Syncthing keeps on the phone; full-text only.
- The database may be shared across machines through Syncthing, Dropbox or
  iCloud Drive, one machine at a time.

## Capabilities and Constraints

- The web UI is embedded in the binary (templates, htmx, fonts): no CDN, no
  network needed to render it.
- Every feature works offline with no model configured; LLM and embedding
  endpoints are optional, driven by env vars.
- One web UI serves the browser, the desktop window and the Android app; on
  Android it is read-only and has no semantic search.
- Keyboard-first: every action is reachable from the keyboard, the mouse is
  optional.
- English and French catalogs stay in sync; language follows the browser's or
  phone's `Accept-Language`, English by default.
- Pure Go, no cgo.

## Brand Commitments

- Name: `glane` (from "glaner", to glean). Tagline in the UI: "your watch,
  found in a word".
- Bilingual voice, English and French, terse and technical.
- Geist and Geist Mono are embedded on purpose (SIL OFL), so the UI looks the
  same offline.

## Evidence on Hand

- Real product docs: `README.md`, `docs/*.md`.
- App icons: `internal/web/static/icon-*.png`.
- No testimonials, user counts, benchmarks or press exist; do not invent them.

## Product Principles

1. **Local and owned.** One binary, one file; nothing leaves the machine unless
   the user configures an endpoint.
2. **Offline is the baseline.** Optional layers add value and never become
   requirements; failures fall back quietly.
3. **Fast from the keyboard.** Getting an item back takes a few keystrokes.
4. **One index, one UI.** Sources merge; every surface shows the same UI.
5. **Both find and rediscover.** Exact retrieval and topic browsing get equal
   care.

## Accessibility & Inclusion

Full keyboard operation is a product requirement. English and French are both
first-class.
