# glane — sync framework + GitHub stars connector

**Date :** 2026-07-14
**Statut :** design validé

## Problème

`glane` sait chercher dans une archive Twitter statique importée une fois. Mais
la veille est vivante : les sources actuelles (GitHub stars, plus tard Bluesky
et Mastodon) accumulent du nouveau contenu en continu. Il faut pouvoir aller le
chercher régulièrement, sans tout re-télécharger à chaque fois.

## Objectif

Établir le **pattern de synchronisation réutilisable** (état de curseur +
upsert incrémental + sous-commande `sync`) sur la source la plus simple —
**GitHub stars** — de sorte que Bluesky et Mastodon n'aient plus qu'à réutiliser
le même socle.

## Principes directeurs

- **Incrémental.** Après le premier backfill, chaque sync ne récupère que le
  nouveau, via un curseur persistant par source.
- **Idempotent et sûr en cas d'échec.** Le curseur n'avance qu'après un run
  réussi ; un échec en cours de route ne fait que re-fetcher au prochain run
  (l'`Upsert` sur `(source, source_id)` dédoublonne).
- **Pas d'abstraction spéculative (YAGNI).** Avec un seul connecteur, on ne
  définit pas d'interface `Connector` ni de registre. La partie partagée (état
  de curseur, rythme sync→upsert) est mutualisée ; la glue par-API ne l'est pas.
- **Le reste vient gratuitement.** `enrich`, la recherche (plein-texte +
  sémantique) et l'UI web fonctionnent déjà pour n'importe quelle source.

## Périmètre

**Inclus :** table `sync_state` + helpers `GetCursor`/`SetCursor` ; connecteur
`internal/github` (`Sync`) ; sous-commande `glane sync github` ; auth par
`GITHUB_TOKEN`.

**Exclu (YAGNI ici) :** connecteurs Mastodon/Bluesky (réutiliseront ce socle,
chacun son cycle spec→plan) ; `sync all` (trivial à ajouter au 2e connecteur) ;
résumé LLM (sous-projet orthogonal) ; toute interface `Connector` formelle.

## Architecture

### 1. État de synchronisation (le curseur réutilisable)

Ajout d'une table au schéma existant (`internal/store/store.go`) :

```sql
CREATE TABLE IF NOT EXISTS sync_state (
  source TEXT PRIMARY KEY,
  cursor TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL DEFAULT 0
);
```

Nouveau fichier `internal/store/sync.go` :

- `func (s *Store) GetCursor(source string) (string, error)` — renvoie `""` si
  aucune ligne (première sync).
- `func (s *Store) SetCursor(source, cursor string) error` — upsert de la ligne,
  met `updated_at` à l'epoch courant.

Le curseur est une **chaîne opaque propre à chaque connecteur** : GitHub y
stocke le `starred_at` le plus récent déjà vu. Mastodon y mettra un `max_id`,
Bluesky un jeton de page. C'est tout le « framework ».

### 2. Connecteur GitHub (`internal/github`)

`func Sync(s *store.Store, token string, hc *http.Client) (int, error)` — renvoie
le nombre de nouvelles stars importées.

Requêtes :
```
GET https://api.github.com/user/starred?sort=created&direction=desc&per_page=100
Headers:
  Authorization: Bearer <token>
  Accept: application/vnd.github.star+json      # active le champ starred_at
  X-GitHub-Api-Version: 2022-11-28
```

Réponse (forme `star+json`) : tableau de `{ "starred_at": "...", "repo": { ... } }`.

Mapping vers `store.Item` :

| champ Item | source |
|---|---|
| `Source` | `"github"` |
| `SourceID` | `repo.id` (numérique, stable même après renommage) → string |
| `Kind` | `"star"` |
| `Author` | `repo.owner.login` |
| `Text` | `"{repo.full_name} — {repo.description}"` |
| `URL` | `repo.html_url` |
| `CreatedAt` | `starred_at` parsé (RFC3339) → epoch |

Boucle :
1. Lire le curseur (`GetCursor("github")`).
2. Paginer newest-first. Pour chaque entrée : si `starred_at <= cursor`, on a
   rejoint le déjà-importé → arrêt. Sinon on l'accumule.
3. Continuer tant qu'une page renvoie 100 items (page pleine) et qu'on n'a pas
   atteint le curseur. Première sync (curseur vide) : backfill complet.
4. `Upsert` de tous les items collectés.
5. `SetCursor("github", <starred_at le plus récent vu ce run>)` — **uniquement
   si tout le run a réussi**.

Comparaison du curseur : les `starred_at` sont des timestamps RFC3339 en UTC,
même format → comparaison lexicographique de chaînes suffisante et correcte.

### 3. Câblage CLI

Nouvelle sous-commande dans `main.go` : `glane sync github`.

- Lit `GITHUB_TOKEN`. Absent → `fatal` avec message clair (`set GITHUB_TOKEN`).
- Appelle `github.Sync`, affiche `synced N new stars`.
- Dispatch : `sync` avec un argument de source ; source inconnue → erreur listant
  les sources connues (`github`). `sync all` non implémenté ici.

### 4. Ce qui vient gratuitement

Aucun autre changement : `glane enrich` ira chercher la page du repo étoilé et
en extraira le README comme texte d'article ; la recherche plein-texte +
sémantique et l'UI web gèrent déjà toute source ; les filtres `--source github`
et `--since` fonctionnent déjà.

## Gestion des erreurs

- `GITHUB_TOKEN` absent → `fatal` : « set GITHUB_TOKEN ».
- Réponse `401` → erreur « GitHub auth failed (check GITHUB_TOKEN) ».
- Autre non-`200` → erreur remontée avec le code de statut (et, si dispo, l'info
  de rate limit `X-RateLimit-Reset`).
- Erreur réseau en cours de sync → erreur remontée ; les items déjà upsertés
  persistent, le curseur n'a pas avancé → le run suivant reprend depuis le
  newest (re-fetch idempotent, sans doublon).

## Tests

`go test`, httptest, **aucun accès réseau réel** :

- Backfill multi-pages : le serveur httptest sert 2 pages `star+json` ; `Sync`
  importe tous les items, mappe correctement les champs (id→SourceID,
  owner→Author, starred_at→CreatedAt, description dans Text), et pose le curseur
  au `starred_at` le plus récent.
- Incrémental : après un premier run, un second run où toutes les entrées sont
  `<= cursor` importe 0 nouvel item.
- Arrêt anticipé : une page contenant un mélange (quelques `> cursor`, puis des
  `<= cursor`) n'importe que les plus récents.
- `GetCursor`/`SetCursor` : round-trip, et `GetCursor` d'une source inconnue
  renvoie `""` sans erreur.

Pas de nouvelle dépendance — stdlib `net/http` + `encoding/json` uniquement.

## Ordre de construction

1. `sync_state` (schéma) + `GetCursor`/`SetCursor` + tests store.
2. `internal/github` : mapping + `Sync` (backfill + incrémental) + tests httptest.
3. Sous-commande `glane sync github` dans `main.go` + gestion `GITHUB_TOKEN`.
