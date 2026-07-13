# glane — recherche intelligente dans sa veille

**Date :** 2026-07-13
**Statut :** design validé

## Problème

Je fais ma veille techno sur Bluesky, Mastodon (et Twitter avant, que je
n'utilise plus), plus les stars GitHub. Je like / repost / bookmark des posts
« pour plus tard » et je les oublie. Cette mine d'or est aujourd'hui
inexploitable : éparpillée sur 4 plateformes, sans recherche transverse. Le
plus souvent le post n'est qu'un lien vers un blog — la vraie valeur est dans
l'article, pas dans les 100 caractères du post.

## Objectif

Un outil personnel qui consolide tout ce contenu sauvegardé dans un index
unique et cherchable, en **plein-texte** (toujours) et en **sémantique**
(quand un modèle d'embeddings est disponible).

## Principes directeurs

- **Dégradation gracieuse.** Sans aucun LLM, la recherche plein-texte doit
  marcher entièrement. Le sémantique et les résumés sont des couches bonus.
- **Embeddings = une URL configurable** (API compatible OpenAI). Local
  (Ollama sur un M2) ou distant, on change une variable.
- **Un seul binaire, un seul fichier SQLite.** Pas de service externe, pas de
  base vectorielle, pas de chaîne de build front.
- **Incrémental.** `import twitter` + `search` plein-texte utilisables dès le
  premier jour ; les autres sources et le sémantique se branchent ensuite.
- **Dépendances maigres**, stdlib par défaut.

## Périmètre

**Inclus :** import Twitter (archive statique), connecteurs sync
Bluesky/Mastodon/GitHub, enrichissement des liens (extraction + résumé LLM
optionnel), recherche plein-texte + sémantique, CLI + UI web locale.

**Exclu (YAGNI) :** base vectorielle dédiée, daemon/scheduler maison (on
s'appuie sur cron/launchd), RAG / question-réponse, multi-utilisateur,
authentification de l'UI web (locale uniquement).

## Architecture

Un binaire Go `glane` avec des sous-commandes :

```
glane import twitter <dossier>          # import unique de l'archive statique
glane sync bluesky | mastodon | github  # pull des nouveaux items (idempotent)
glane sync all                          # tout, pour cron/launchd
glane enrich                            # fetch liens, extrait le texte, (option) résume + embed
glane search "<requête>" [--source X] [--since 2023] [--limit N]
glane serve [--port 8080]               # UI web locale
```

- Pas de daemon ni de scheduler maison : `sync all` est idempotent ; la
  planification se fait via une entrée cron/launchd documentée dans le README.
- Config par variables d'environnement, complétées par un
  `~/.config/glane/config.toml` optionnel : URL + modèle d'embeddings, URL +
  modèle du LLM de résumé, tokens/mots de passe d'app des connecteurs.

## Modèle de données (SQLite)

### Table `items`

| colonne | type | note |
|---|---|---|
| `id` | integer PK | |
| `source` | text | `twitter` / `bluesky` / `mastodon` / `github` |
| `source_id` | text | id natif de la plateforme |
| `kind` | text | `like` / `repost` / `star` / `own` |
| `author` | text | auteur du post/repo |
| `text` | text | texte du post (ou description du repo) |
| `url` | text | lien vers le post/repo d'origine |
| `created_at` | integer | epoch, date de création côté plateforme |

Unicité `(source, source_id)` → **upsert**, donc re-sync sans doublons.

### Colonnes d'enrichissement (sur `items`, pour le lien principal)

`link_url`, `article_title`, `article_text`, `article_summary`,
`fetch_status` (`pending` / `ok` / `failed`), `fetched_at`.

> `ponytail:` on ne traite que le **premier** lien du post. Les posts
> multi-liens sont rares ; on ajoutera une table `links` séparée seulement si
> le besoin se confirme.

### Table `items_fts` (FTS5)

Index plein-texte sur `text + article_title + article_text + article_summary +
author`. Tenu à jour par triggers sur `items`. C'est le moteur plein-texte,
toujours disponible.

### Table `embeddings`

`item_id` (FK), `vector` (blob de float32), `model`. Chargés en mémoire au
moment de la recherche pour un cosinus brute-force.

> `ponytail:` cosinus brute-force en mémoire — instantané jusqu'à ~100k items,
> très au-dessus de la volumétrie attendue. Passer à sqlite-vec seulement si
> ça devient lent.

## Recherche

- **Plein-texte** : requête FTS5, tri BM25. Marche toujours, offline, zéro LLM.
- **Sémantique** : si l'URL d'embeddings répond → embed la requête, cosinus
  contre tous les vecteurs stockés, tri par similarité.
- **Combiné** : quand les deux sont disponibles, fusion des deux classements
  par **Reciprocal Rank Fusion** (RRF, `score = Σ 1/(k+rang)`, `k=60`). Pas
  d'embeddings disponibles → plein-texte seul, de façon transparente.

Filtres appliqués en amont sur `items` : `--source`, `--since`, `--limit`.

## Connecteurs

Chaque connecteur fait un **upsert** sur `(source, source_id)` et garde un
curseur pour ne récupérer que le nouveau au prochain run.

- **Twitter** (import unique) : parse les fichiers `window.YTD.*.part0 = [...]`
  (on retire le préfixe d'affectation, puis `json.Unmarshal`). `like.js` →
  `kind=like` ; `tweets.js` → `kind=own` ou `repost`. Le texte utile est
  `fullText` ; l'URL utile est le lien t.co contenu dans le texte.
- **GitHub stars** : `GET /users/{user}/starred` (paginé). `text` = description
  du repo, `url` = repo ; l'enrichissement peut aller chercher le README.
- **Bluesky** : AT Protocol (app password) — `app.bsky.feed.getActorLikes` +
  reposts.
- **Mastodon** : token — `/api/v1/favourites` + `/api/v1/bookmarks`.

## Enrichissement

Pipeline reprenable qui ne traite que les items en `fetch_status = pending` :

1. Résoudre le lien principal (suivre les redirections t.co, profondeur bornée).
2. Fetch le HTML.
3. Extraire le corps de l'article (`go-shiori/go-readability`) → `article_text`.
4. **(option)** Résumé via LLM compatible OpenAI → `article_summary`.
5. **(option)** Embedding sur `titre + résumé` (ou titre + texte tronqué si pas
   de résumé) → table `embeddings`.

Chaque étape optionnelle est sautée proprement si non configurée. Lien mort ou
extraction impossible → `fetch_status = failed`, le post reste indexé par son
texte. Le contenu extrait est mis en cache (stocké) pour ne jamais refetch.

## Gestion des erreurs

- **Liens morts** (fréquents sur les t.co de 2022) : `fetch_status=failed`,
  l'item reste cherchable via son texte.
- **Embeddings / LLM indisponibles** : étape sautée, item pleinement cherchable
  en plein-texte.
- **Rate limits API** : backoff ; le curseur permet de reprendre où on en était.
- **Import Twitter partiel** : upsert idempotent, on peut relancer.

## UI web

`glane serve` sert une page unique en `html/template` :

- **htmx** pour la recherche instantanée (pas de framework JS, pas de build).
- Résultats : badge source, date, auteur, extrait (résumé sinon texte), lien
  sortant, filtres source/date.
- Assets (htmx + CSS) embarqués via `embed.FS` — le binaire est autonome.
- Locale uniquement, pas d'authentification.

## Dépendances

- `modernc.org/sqlite` — SQLite en Go pur, FTS5 inclus, **pas de cgo**
  (cross-compile, binaire unique).
- `go-shiori/go-readability` — extraction du corps d'article.
- Stdlib pour le reste : `net/http` (clients API, embeddings, serveur),
  `html/template`, `flag` (dispatch des sous-commandes), `encoding/json`.
- htmx : un seul fichier JS vendored/embarqué (pas de npm).

## Tests

Un check runnable par pièce non-triviale, via `go test` et petites fixtures :

- parsing d'une archive Twitter (fixture réduite `like.js` / `tweets.js`) ;
- cosinus + tri par similarité ;
- fusion RRF de deux classements ;
- requête FTS5 sur une base seedée.

Pas de framework, pas de fixtures lourdes.

## Ordre de construction

1. Socle : schéma SQLite + FTS5, `import twitter`, `search` plein-texte, CLI.
2. `serve` (UI web) sur le plein-texte.
3. Enrichissement : fetch + extraction (sans LLM).
4. Sémantique : embeddings + cosinus + fusion RRF.
5. Connecteurs sync : GitHub, puis Mastodon, puis Bluesky.
6. Résumé LLM (couche finale) + ligne cron/launchd documentée.
