# glane — Claude Code plugin

Lets Claude Code (and other agents) search your personal tech-watch — the posts
you liked, reposted, bookmarked, and starred, plus the articles behind them —
straight from your `glane` install, so answers are grounded in *your* curated
sources instead of the model's training data.

The plugin ships a single **read-only** skill: it only runs `glane search` and
`glane tags`. Read-only is *enforced*, not just requested — a bundled
`PreToolUse` hook blocks any `glane` command that mutates data or runs long
(`sync`, `enrich`, `summarize`, `update`, `import`, `serve`), so those stay
off-limits even if the agent tries.

## Install

From Claude Code:

```
/plugin marketplace add jcgay/glane
/plugin install glane@glane
```

## Prerequisites

The skill shells out to your local `glane`, so you need:

- the `glane` binary on your `PATH` (see the repo README to build it), and
- a populated database (`glane import …` / `glane sync …` at least once).

Point it at a non-default database with `GLANE_DB=/path/to/glane.db` in the
environment where Claude Code runs, if you don't use the default location.

## What the agent will do

When a question touches a topic you follow, the agent runs e.g.:

```sh
glane search kubernetes networking --limit 10
glane search --tag rust
glane tags
```

reads the results, and follows the source URLs for full content. See
[`skills/glane/SKILL.md`](skills/glane/SKILL.md) for the exact guidance it loads.
