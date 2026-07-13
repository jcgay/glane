# glane — Mastodon & Bluesky connectors + `sync all`

**Date :** 2026-07-14
**Statut :** design validé

## Problème

`glane` synchronise déjà les stars GitHub via un framework de sync réutilisable
(table `sync_state` + curseur opaque par flux + upsert incrémental). Restent les
deux réseaux sociaux au cœur de la veille de l'utilisateur : Mastodon (favoris +
marque-pages) et Bluesky (likes). Ils doivent réutiliser le même socle.

## Objectif

Ajouter les connecteurs `sync mastodon` et `sync bluesky`, plus `sync all` (qui
lance tous les connecteurs configurés), en réutilisant le framework existant.
La recherche, l'enrichissement et l'UI web fonctionnent déjà pour toute source.

## Principes directeurs

- **Réutilisation du framework.** Aucun changement de schéma : `sync_state` avec
  une clé de curseur opaque suffit. Une source peut détenir plusieurs curseurs.
- **Idempotence comme filet.** L'incrémental diffère par plateforme et n'est pas
  parfait ; l'`Upsert` sur `(source, source_id)` garantit qu'au pire on re-fetch,
  jamais de donnée corrompue. Chaque curseur n'avance qu'après le run réussi de
  son flux.
- **Config par variables d'environnement**, comme `GITHUB_TOKEN`. Config absente
  → `fatal` clair.
- **Pas d'abstraction spéculative.** Chaque connecteur est un package + une
  fonction `Sync`, comme `internal/github`. Pas d'interface `Connector`.

## Périmètre

**Inclus :** `internal/mastodon` (favoris + marque-pages), `internal/bluesky`
(likes), sous-commandes `sync mastodon` / `sync bluesky` / `sync all`.

**Exclu (YAGNI) :** reposts Bluesky (pas d'endpoint simple ; les likes sont le
signal) ; marque-pages Bluesky (inexistants côté serveur) ; résumé LLM
(sous-projet orthogonal) ; entrée cron/launchd documentée (à faire quand
`sync all` sera utilisé en routine — hors code).

## Clés de curseur (multi-flux)

`GetCursor`/`SetCursor` prennent une chaîne opaque. On l'utilise comme
identifiant de flux, distinct du `Source` de l'item :

| clé de curseur | Source des items | contenu du curseur |
|---|---|---|
| `github` | `github` | `starred_at` le plus récent (existant) |
| `mastodon:favourites` | `mastodon` | id du statut favori le plus récent |
| `mastodon:bookmarks` | `mastodon` | id du statut marque-pagé le plus récent |
| `bluesky:likes` | `bluesky` | AT-URI du post liké le plus récent |

Aucune migration : la table `sync_state(source, cursor, updated_at)` existe déjà,
`source` sert ici de clé de flux.

## Connecteur Mastodon (`internal/mastodon`)

Config : `GLANE_MASTODON_URL` (base de l'instance, ex. `https://mastodon.social`),
`GLANE_MASTODON_TOKEN`.

`func Sync(s *store.Store, baseURL, token string, hc *http.Client) (int, error)` —
synchronise les DEUX flux (favoris puis marque-pages) et renvoie le total importé.

Pour chaque flux (`GET {base}/api/v1/favourites`, `GET {base}/api/v1/bookmarks`)
avec `Authorization: Bearer <token>` (scopes `read:favourites` / `read:bookmarks`) :

- Pagination via l'en-tête **`Link`** : suivre telle quelle l'URL `rel="next"`
  jusqu'à absence de `next`. **Important (vérifié dans la doc)** : les `max_id`/
  `min_id` de l'en-tête `Link` de ces endpoints sont des **ids de Favourite
  internes opaques**, PAS des ids de statut — donc on ne construit jamais
  `?min_id=<status.id>` soi-même ; on suit uniquement les liens pré-construits.
- Incrémental : on utilise l'`id` du **statut** (lui, exposé et comparable) pour
  savoir où s'arrêter. Lire le curseur du flux ; paginer du plus récent au plus
  ancien via les liens `next` et **s'arrêter dès qu'un statut a un `id` ≤ curseur**
  (comparaison numérique : les ids Mastodon sont des entiers snowflake encodés en
  chaîne ; parser en `int64` — si non numérique, repli sur arrêt à l'égalité
  stricte). Curseur vide → backfill complet.
- Après succès du flux : `SetCursor(<clé du flux>, <id du statut le plus récent
  vu>)`. Les items Mastodon reviennent du plus récent au plus ancien ; le plus
  récent est le premier de la première page.

Mapping Status → `store.Item` :

| champ | source |
|---|---|
| `Source` | `"mastodon"` |
| `SourceID` | `status.id` |
| `Kind` | `"like"` (favoris) ou `"bookmark"` (marque-pages) |
| `Author` | `status.account.acct` |
| `Text` | `status.content` **débarrassé de ses balises HTML** |
| `URL` | `status.url` |
| `CreatedAt` | `status.created_at` (RFC3339) → epoch |

Le contenu Mastodon est du HTML : on retire les balises pour le texte cherchable
(un helper `stripHTML` minimal en stdlib) ; les liens qu'il contient nourrissent
`enrich` (comportement source-agnostique existant : `FirstURL(text)` sinon
`it.URL`).

## Connecteur Bluesky (`internal/bluesky`)

Config : `GLANE_BLUESKY_HANDLE`, `GLANE_BLUESKY_APP_PASSWORD` (un app password,
pas le mot de passe principal).

`func Sync(s *store.Store, handle, appPassword string, hc *http.Client) (int, error)`.

- Auth : `POST {pds}/xrpc/com.atproto.server.createSession` avec
  `{identifier: handle, password: appPassword}` → `accessJwt` (+ `did`). Base
  PDS par défaut `https://bsky.social`.
- `GET {pds}/xrpc/app.bsky.feed.getActorLikes?actor=<handle>&limit=100&cursor=…`
  avec `Authorization: Bearer <accessJwt>` (endpoint **authentifié** ; `actor`
  doit être le compte appelant) ; pagination via le champ `cursor` opaque de la
  réponse jusqu'à absence de `cursor`. Réponse : `{ feed: [{ post: {...} }],
  cursor }` — chaque `feed[i].post` est un `postView` (`uri`, `cid`, `author`,
  `record`, `indexedAt`).
- Incrémental : les likes reviennent du plus récent au plus ancien. Lire le
  curseur (`bluesky:likes` = AT-URI du post liké le plus récent au run
  précédent) ; **arrêter la pagination dès qu'on rencontre ce URI**. S'il
  n'apparaît pas (post dé-liké ou supprimé), on pagine jusqu'au bout ; l'`Upsert`
  dédoublonne, donc c'est sans risque.
- Après succès : `SetCursor("bluesky:likes", <URI du post le plus récent vu>)`.

Mapping post liké → `store.Item` :

| champ | source |
|---|---|
| `Source` | `"bluesky"` |
| `SourceID` | `post.uri` (AT-URI, stable) |
| `Kind` | `"like"` |
| `Author` | `post.author.handle` |
| `Text` | `post.record.text` |
| `URL` | permalien `https://bsky.app/profile/{author.handle}/post/{rkey}` (rkey = dernier segment de l'AT-URI) |
| `CreatedAt` | `post.record.createdAt` (RFC3339) → epoch |

## `sync all` + CLI

Étendre `cmdSync` :

- `sync mastodon` → lit `GLANE_MASTODON_URL` + `GLANE_MASTODON_TOKEN` ; absents →
  `fatal`. Appelle `mastodon.Sync`, affiche `synced N new mastodon items`.
- `sync bluesky` → lit `GLANE_BLUESKY_HANDLE` + `GLANE_BLUESKY_APP_PASSWORD` ;
  absents → `fatal`. Affiche `synced N new bluesky items`.
- `sync all` → lance chaque connecteur **dont la config est présente**, saute
  (sans erreur) ceux non configurés en indiquant lesquels, agrège les comptes.
  Une erreur d'un connecteur est remontée mais n'empêche pas les autres de
  tourner (les résultats déjà obtenus persistent, curseurs déjà avancés inclus).
- Source inconnue → erreur listant les sources connues (`github`, `mastodon`,
  `bluesky`, `all`).

## Gestion des erreurs

- Variables de config absentes → `fatal` avec le nom exact des variables à
  définir.
- `401` → message clair par plateforme (`Mastodon auth failed (check
  GLANE_MASTODON_TOKEN)`, `Bluesky auth failed (check GLANE_BLUESKY_APP_PASSWORD)`).
- Autre non-`200` → erreur remontée avec le code de statut.
- Chaque curseur n'avance qu'après le run réussi de son flux ; un échec laisse le
  curseur inchangé → re-fetch idempotent au prochain run.

## Tests

`go test`, **httptest uniquement, aucun accès réseau réel** :

- **Mastodon** : serveur httptest servant 2 pages liées par en-tête `Link`
  (`rel="next"`) ; `Sync` importe tout, mappe les champs, retire le HTML du
  contenu, distingue `like` vs `bookmark` selon l'endpoint, pose les deux
  curseurs. Second run avec `min_id` : un flux dont toutes les entrées sont
  connues importe 0.
- **Bluesky** : serveur httptest gérant `createSession` (renvoie un JWT) puis
  `getActorLikes` paginé ; `Sync` s'authentifie, importe les likes, construit le
  permalien `bsky.app`, s'arrête au URI curseur (incrémental), et échoue
  proprement si `createSession` renvoie 401.
- Round-trip des curseurs multi-flux déjà couvert par `sync_test.go`.

Pas de nouvelle dépendance — stdlib (`net/http`, `encoding/json`, `regexp`,
`strings`, `html`, `time`) uniquement. **Ne pas** ajouter
`golang.org/x/net/html`. `stripHTML` reste en stdlib : retirer les balises avec
une regex `<[^>]*>` puis décoder les entités avec `html.UnescapeString` — on ne
rend pas le HTML, on produit juste du texte cherchable, donc une suppression
naïve suffit.

## Note d'implémentation — API vérifiées

Détails confirmés contre la doc officielle le 2026-07-14 :

- **Mastodon `/api/v1/favourites` & `/bookmarks`** (docs.joinmastodon.org) :
  renvoient un tableau de Status ; pagination par en-tête `Link`
  (`rel="next"`/`rel="prev"`). Les ids `max_id`/`min_id` du `Link` sont des ids
  de Favourite **internes opaques**, pas des ids de statut → suivre les liens
  pré-construits, ne pas les fabriquer. L'`id` du statut, lui, est exposé et sert
  d'ancre d'arrêt incrémental.
- **Bluesky** (lexicons atproto) : `com.atproto.server.createSession` prend
  `{identifier, password}` et renvoie `{accessJwt, refreshJwt, handle, did}`.
  `app.bsky.feed.getActorLikes` (auth requise, `actor` = compte appelant) prend
  `actor`/`limit`(1–100)/`cursor` et renvoie `{feed: [feedViewPost], cursor}` ;
  `feedViewPost.post` = `postView` (`uri`, `cid`, `author` = `profileViewBasic`
  avec `did`/`handle`/`displayName`, `record` = objet ouvert contenant
  `app.bsky.feed.post` avec `text`/`createdAt`, `indexedAt`).

L'idempotence de l'`Upsert` reste le filet pour tout écart d'incrémental.

## Ordre de construction

1. `internal/mastodon` : `stripHTML` + mapping + `Sync` (2 flux, Link pagination,
   min_id incrémental) + tests httptest.
2. `internal/bluesky` : auth session + mapping + `Sync` (getActorLikes, cursor
   paging, stop-at-URI) + tests httptest.
3. CLI : `sync mastodon`, `sync bluesky`, `sync all` (skip non-configurés) dans
   `main.go`.
