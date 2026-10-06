# Semantic search

Semantic search lets you find a post by meaning even when you've forgotten its
exact words. It's entirely optional and driven by three environment variables. If
`GLANE_EMBED_URL` is unset, `glane` stays full-text only.

| Variable | Meaning |
|----------|---------|
| `GLANE_EMBED_URL` | Base URL of an **OpenAI-compatible** embeddings API. Unset → semantic disabled. |
| `GLANE_EMBED_MODEL` | Embedding model name |
| `GLANE_EMBED_KEY` | API key (sent as `Authorization: Bearer …`); omit for local endpoints |

`glane` calls `POST {GLANE_EMBED_URL}/embeddings` with `{ "model": …, "input": [...] }`.

## Local, with Ollama

Run a model on your machine (e.g. an M2) and point `glane` at it — free, private,
offline:

```sh
export GLANE_EMBED_URL=http://localhost:11434/v1
export GLANE_EMBED_MODEL=nomic-embed-text

./glane enrich          # generates embeddings while extracting articles
./glane search "how to reduce container image size"
```

## Remote API

```sh
export GLANE_EMBED_URL=https://api.openai.com/v1
export GLANE_EMBED_MODEL=text-embedding-3-small
export GLANE_EMBED_KEY=sk-…
```

Switching between local and remote is just changing these variables.

## Choosing a model

The examples above use `nomic-embed-text`, which is English-centric. If your
watch mixes languages — e.g. French and English posts — pick a **multilingual**
model so a French query can match an English article and vice versa:

- **`bge-m3`** (Ollama, ~1 GB, runs on CPU) — strong multilingual quality,
  recommended default for a mixed-language corpus.
- **`nomic-embed-text`** / **`mxbai-embed-large`** (Ollama) — excellent but
  English-leaning; use only if your corpus is essentially English.
- **`text-embedding-3-small`** (OpenAI) — cheap remote option, decent
  multilingual coverage, if you'd rather not run a model locally.

For `summarize`, the task is light (a short summary + tags), so any small
instruct model works — `gemma3`, `qwen2.5`, or `llama3.1` via Ollama.

> Vectors are generated during `enrich` and keyed by model name. After changing
> the embedding model, re-run `enrich` so the stored vectors match your query
> model — `search` warns on stderr if it finds vectors only under other models.
