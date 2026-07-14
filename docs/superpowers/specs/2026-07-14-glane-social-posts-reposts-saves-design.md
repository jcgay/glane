# glane — Bluesky saves + my posts/reposts (Mastodon & Bluesky)

**Date :** 2026-07-14
**Statut :** design validé

## Problème

Les connecteurs sociaux ne couvrent qu'une partie de la veille :
- **Bluesky** : on récupère les likes, mais PAS les posts **sauvegardés**
  (bookmarks Bluesky — que l'utilisateur utilise réellement).
- **Mastodon & Bluesky** : on ne récupère PAS **ses propres posts** ni ses
  **reposts/boosts**.

Ces contenus font partie de la veille (ce qu'on publie/repartage/garde pour
plus tard) et doivent être indexés comme le reste.

## Objectif

Compléter les deux connecteurs pour couvrir, en plus de l'existant :
- Bluesky : posts sauvegardés, mes posts, mes reposts.
- Mastodon : mes posts, mes boosts.

## Principes directeurs

- **Réutilisation du framework** : chaque nouveau flux est un flux paginé de plus
  avec sa propre clé de curseur (`sync_state`) ; aucun changement de schéma.
- **Un seul enregistrement par post** (décision validée) : un post touché par
  plusieurs relations (liké ET reposté, etc.) reste UNE ligne
  `(source, source_id)`. Le contenu est enrichi/résumé/embed une seule fois et
  n'apparaît qu'une fois en recherche.
- **Précédence par ordre de flux** : dans chaque connecteur, les flux tournent
  dans un ordre fixe (like → bookmark → posts/reposts) ; l'`Upsert` existant
  écrase `kind`, donc le dernier flux gagne → posts/reposts > bookmark > like.
  Déterministe sous `sync all`. Aucun changement d'`Upsert`.
- **Idempotence** : chaque nouveau curseur n'avance qu'après le succès de son
  flux ; re-fetch idempotent via `Upsert`.

## Périmètre

**Inclus :** flux Bluesky `bookmarks` + `authorfeed` (own/repost) dans
`internal/bluesky` ; flux Mastodon `authorfeed` (own/boost) dans
`internal/mastodon`. Aucune nouvelle sous-commande (les flux s'ajoutent aux
`Sync` existants, donc `sync bluesky` / `sync mastodon` / `sync all` les prennent
automatiquement).

**Exclu (YAGNI) :** modèle multi-relations (une ligne = une relation) ; capture
des liens externes Bluesky `embed.external` (gap connu, séparé) ; mes réponses
(replies) — volontairement exclues (voir choix délibérés).

## API vérifiées (docs officielles / lexicons atproto, 2026-07-14)

- **Bluesky `app.bsky.bookmark.getBookmarks`** (auth requise) : params
  `limit`(1–100, def 50), `cursor` ; sortie `{cursor, bookmarks:[bookmarkView]}`.
  `bookmarkView = {subject (strong ref), createdAt, item}` où `item` est une
  **union** : `app.bsky.feed.defs#postView` | `#blockedPost` | `#notFoundPost`.
  → ne traiter que le cas `postView` (discriminé par `$type`), ignorer les deux
  autres.
- **Bluesky `app.bsky.feed.getAuthorFeed`** (auth non requise, mais on utilise le
  JWT) : params `actor`(requis), `limit`(1–100), `cursor`,
  `filter` (enum : `posts_with_replies`, `posts_no_replies`, `posts_with_media`,
  `posts_and_author_threads`, `posts_with_video`). Sortie `{cursor, feed:[feedViewPost]}`.
  `feedViewPost = {post: postView, reason?}` ; `reason.$type ==
  "app.bsky.feed.defs#reasonRepost"` marque un repost de l'acteur.
- **Mastodon** : `GET /api/v1/accounts/verify_credentials` (auth) → l'objet
  compte de l'utilisateur courant (champ `id`). Puis
  `GET /api/v1/accounts/{id}/statuses` : tableau de Status, pagination par
  en-tête `Link` (ids opaques — suivre `rel="next"`, arrêt sur `status.id`
  comme favourites). Param `exclude_replies`. Un Status dont `reblog` est
  non-null est un boost (le Status imbriqué `reblog` est le statut original).

## Bluesky (`internal/bluesky`)

`Sync` enchaîne, dans cet ordre : **likes (existant) → bookmarks → authorfeed**.
Chaque flux : lit son curseur, pagine du plus récent au plus ancien, s'arrête au
`post.uri` du curseur, upsert son lot, avance son curseur après succès. (On passe
à un `Upsert` par flux plutôt qu'un seul en fin de `Sync`, pour préserver la
précédence par ordre.)

Nouveau flux **bookmarks** (`bluesky:bookmarks`) :
- `GET {pds}/xrpc/app.bsky.bookmark.getBookmarks?limit=100&cursor=…`, Bearer JWT.
- Pour chaque `bookmarkView` : si `item.$type != "app.bsky.feed.defs#postView"`
  → ignorer. Sinon mapper le `postView` :
  `Item{Source:"bluesky", SourceID: item.uri, Kind:"bookmark", Author:
  item.author.handle, Text: item.record.text, URL: permalink(handle, uri),
  CreatedAt: item.record.createdAt}`.

Nouveau flux **authorfeed** (`bluesky:authorfeed`) :
- `GET {pds}/xrpc/app.bsky.feed.getAuthorFeed?actor=<handle>&limit=100&filter=posts_no_replies&cursor=…`, Bearer JWT.
- Pour chaque `feedViewPost` : `kind = "own"` par défaut ; si
  `reason.$type == "app.bsky.feed.defs#reasonRepost"` → `kind = "repost"`.
  Mapper le `post` (postView) : `SourceID: post.uri, Author: post.author.handle,
  Text: post.record.text, URL: permalink(post.author.handle, post.uri),
  CreatedAt: post.record.createdAt`.
- Curseur = `post.uri` du premier item (le plus récent) ; arrêt quand
  `post.uri == cursor`.

Le décodage `getActorLikes`/`getAuthorFeed`/`getBookmarks` partage la même forme
`postView` (`uri`, `author.handle`, `record.text`, `record.createdAt`) — un type
de post commun peut être réutilisé.

## Mastodon (`internal/mastodon`)

`Sync` enchaîne : **favourites (existant) → bookmarks (existant) → authorfeed**.

Nouveau flux **authorfeed** (`mastodon:authorfeed`) :
- D'abord `GET {base}/api/v1/accounts/verify_credentials` (Bearer token) →
  `id` du compte. 401 → `Mastodon auth failed (check MASTODON_ACCESS_TOKEN)`.
- Puis paginer `GET {base}/api/v1/accounts/{id}/statuses?exclude_replies=true&limit=40`
  via l'en-tête `Link` (`rel="next"`), arrêt quand un `status.id` (id de
  l'entrée de flux, own ou boost) est `<= curseur` (comparaison numérique, comme
  favourites).
- Mapping : si `status.reblog != null` (boost) → `kind:"repost"`, champs pris du
  **statut reblogué** : `SourceID: reblog.id, Author: reblog.account.acct, Text:
  stripHTML(reblog.content), URL: reblog.url, CreatedAt: reblog.created_at`.
  Sinon → `kind:"own"`, champs du statut lui-même (id/acct/content/url/created_at).
- Curseur `mastodon:authorfeed` = `status.id` (top-level) le plus récent vu ;
  il pilote la position incrémentale indépendamment du `SourceID` stocké (qui
  peut être `reblog.id` pour un boost).

## Dédup & kind

Aucun changement de schéma ni d'`Upsert`. La clé unique reste
`(source, source_id)`. Grâce à l'ordre fixe des flux et à l'écrasement de `kind`
par l'`Upsert`, un post multi-relations conserve le kind du dernier flux
(posts/reposts > bookmark > like). Les `kind` possibles : `like`, `bookmark`,
`repost`, `own`, `star`.

## Choix délibérés (validés)

- **Replies exclues** : `filter=posts_no_replies` (Bluesky) et
  `exclude_replies=true` (Mastodon) — « mes posts » = des posts, pas chaque
  réponse.
- **`CreatedAt` = date de création du contenu** (pour un repost/boost : la date
  du post original, cohérent avec le fait qu'on stocke le contenu original).
- **Un repost stocke le post original** (URI/id, auteur, texte) avec
  `kind:"repost"` — retrouvable par ce qu'il pointe réellement.
- **Bookmarks Bluesky illisibles** (`blockedPost`/`notFoundPost`) : ignorés.

## Gestion des erreurs

- 401 → message clair par plateforme. Autre non-200 → erreur avec le code.
- Chaque flux : erreur → retour avant l'avancée de son curseur ; les autres flux
  déjà terminés ont persisté (leurs items + curseurs). `Upsert` idempotent.

## Tests

`go test`, httptest, **aucun accès réseau réel** :

- **Bluesky bookmarks** : serveur httptest renvoyant createSession puis
  getBookmarks avec un `postView`, un `blockedPost` et un `notFoundPost` → seul
  le postView est importé (`kind:"bookmark"`), champs mappés, permalien construit.
- **Bluesky authorfeed** : feed mixant un post sans `reason` (→ `own`) et un post
  avec `reason.$type = #reasonRepost` (→ `repost`) ; vérifier les deux kinds,
  le stop-at-URI incrémental, et que `filter=posts_no_replies` est bien envoyé.
- **Mastodon authorfeed** : verify_credentials → id ; statuses mélangeant un
  statut simple (→ `own`) et un statut avec `reblog` non-null (→ `repost` mappé
  sur le statut reblogué) ; pagination `Link`, stop-at-status-id, HTML strippé.
- **Précédence** : un même `post.uri` importé via likes puis via authorfeed
  (repost) finit avec `kind:"repost"` (le dernier flux gagne) — un test dédié qui
  upsert dans l'ordre et vérifie le kind final.

Pas de nouvelle dépendance — stdlib uniquement (réutilise `stripHTML`,
`permalink`, les patterns de pagination existants).

## Ordre de construction

1. **Bluesky** : refactor `Sync` en flux ordonnés (Upsert par flux) ; ajouter
   `bookmarks` + `authorfeed` (own/repost) + tests httptest.
2. **Mastodon** : ajouter `verify_credentials` + le flux `authorfeed`
   (own/boost, mapping reblog) + tests httptest.
3. (Aucun changement CLI : les flux sont pris par les `sync` existants ; vérifier
   d'un `go build`/`go test` global et d'un run manuel de la surface `sync`.)
