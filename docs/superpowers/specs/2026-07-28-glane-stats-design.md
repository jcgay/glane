# glane stats — design

## Objectif

Donner un aperçu rapide de ce que contient la base glane : combien d'items ont été
importés/synchronisés, combien ont été enrichis (extraction d'article), résumés/tagués,
et vectorisés (embeddings) — ainsi que la fraîcheur du sync par source. Disponible à la
fois comme commande CLI (`glane stats`) et comme page web (`/stats`), en s'appuyant sur
les mêmes données.

## Modèle de données

Aucun changement de schéma. Tous les chiffres sont dérivés des tables existantes :
`items`, `embeddings`, `item_tags`, `sync_state`.

### `internal/store/stats.go`

```go
type SourceCount struct {
    Source string
    Count  int
}

type SourceSync struct {
    Source    string
    UpdatedAt int64 // unix seconds, 0 = jamais synchronisé
}

type Stats struct {
    Total          int
    BySource       []SourceCount // triés par count décroissant, puis source croissante
    Enriched       int           // items avec fetch_status = 'ok'
    Summarized     int           // items avec article_summary != ''
    Embedded       int           // item_id distincts dans embeddings
    DistinctTags   int           // tag distincts dans item_tags
    LastSyncBySource []SourceSync // une ligne par source présente dans sync_state
}

func (s *Store) Stats() (Stats, error)
```

Notes d'implémentation :
- `BySource` : `SELECT source, COUNT(*) FROM items GROUP BY source ORDER BY COUNT(*) DESC, source`.
- `Enriched` : `SELECT COUNT(*) FROM items WHERE fetch_status = 'ok'`. C'est le marqueur
  de succès déjà utilisé par `internal/enrich` (voir `store/enrich.go`
  `SaveEnrichment`) ; il exclut volontairement `'error'` et les lignes vides/en attente.
- `Summarized` : `SELECT COUNT(*) FROM items WHERE article_summary != ''`.
- `Embedded` : `SELECT COUNT(DISTINCT item_id) FROM embeddings`.
- `DistinctTags` : `SELECT COUNT(DISTINCT tag) FROM item_tags`.
- `LastSyncBySource` : `SELECT source, updated_at FROM sync_state ORDER BY source`.
- Toutes les requêtes s'exécutent sur le `*sql.DB` existant ; pas besoin de nouveaux
  index à cette échelle (fichier SQLite local mono-utilisateur).
- Suit la même philosophie de dégradation silencieuse que `TagCounts`/`AttachTags`,
  mais uniquement côté appelant (handler web), pas dans `Stats()` elle-même :
  `Stats()` retourne une vraie erreur comme les autres méthodes du store (`SearchFTS`,
  `TagCounts`), et chaque appelant décide comment la gérer (CLI : `fatal` ; web : log
  + affichage best-effort avec des valeurs à zéro).

## Commande CLI

### `main.go`

- Ajouter `"stats"` au message d'usage et au `switch os.Args[1]`, en appelant
  `cmdStats(s, os.Args[2:])`.
- `cmdStats` parse un petit `flag.FlagSet` avec un seul flag : `-json` (bool, défaut false).
- Sortie texte par défaut — alignée, pas de séparation stdout/stderr applicable ici
  puisqu'il s'agit d'un résumé ponctuel et non d'une commande à progression ; tout
  part sur stdout. **Les libellés de la sortie CLI sont en anglais**, cohérent avec
  les autres commandes (`cmdImport` : `"imported %d likes, %d tweets"`, `cmdTags`,
  etc.) — seule l'UI web est en français :

```
Total          1234 items
  twitter       800
  github        300
  mastodon      100
  bluesky        34
Enriched        950 / 1234
Summarized      600 / 1234
Embeddings      950
Tags            42 distinct
Last sync
  github        2h ago
  mastodon      1d ago
  bluesky       never
```
  - Les sources sans ligne `sync_state` (jamais synchronisées, ex. `twitter` qui est
    import-only) sont omises de la section "Last sync" — seules les sources
    réellement présentes dans `sync_state` sont listées (`twitter` n'y apparaît
    jamais, car c'est un import d'archive ponctuel, pas un connecteur de sync live).
  - Le formatage du temps relatif pour la CLI utilise un texte en anglais
    (`2h ago`, `1d ago`, `never`) — logique similaire à `reltime` côté web mais pas
    la même fonction, puisque `reltime` produit du français (`"il y a 2h"`). Ce sont
    deux implémentations distinctes avec des sorties dans des langues différentes
    (voir ci-dessous).

- Sortie `-json` : `json.MarshalIndent` de la struct `store.Stats` directement (noms
  de champs par défaut via les tags JSON de Go — pas besoin de tags personnalisés
  puisqu'il s'agit d'une struct de reporting en lecture seule, pas d'un contrat d'API).

### Aide au temps relatif (CLI, en anglais)

`reltime` (dans `internal/web/web.go`) produit du français et reste dédiée à l'UI
web — elle n'est pas réutilisée telle quelle. `main.go` reçoit sa propre petite
fonction privée `relTime(ts int64) string` qui reprend la même logique de seuils
(minute/heure/jour/date absolue) mais avec une sortie en anglais (`"2h ago"`,
`"1d ago"`, `"never"` pour `ts <= 0`), cohérente avec le reste de la sortie CLI.
C'est une implémentation séparée plutôt qu'un helper partagé paramétré par langue —
ni `store` ni `internal/web` n'ont besoin de connaître l'existence de l'autre
consommateur, et ~15 lignes dupliquées ne justifient pas un package partagé.

## Interface web

### Route

`internal/web/web.go` : ajouter `mux.HandleFunc("/stats", ...)` appelant `s.Stats()`,
avec log + poursuite en cas d'erreur (affiche la page avec des valeurs à zéro plutôt
qu'un 500 dur, cohérent avec la philosophie de "dégradation silencieuse" déjà utilisée
ailleurs pour des données optionnelles), puis exécute un nouveau template `stats.html`.

### Template (`internal/web/templates/stats.html`)

Même langage visuel que `index.html` (mêmes variables CSS, réutilise les propriétés
personnalisées du `<style>` existant en vivant dans le même ensemble de templates, donc
`template.FuncMap` et les variables CSS embarquées sont disponibles). Mise en page :
une grille de petites cartes de statistiques (Total, Enrichis, Résumés, Embeddings,
Tags) au-dessus d'un tableau de répartition "Par source" et d'une liste "Dernier
sync". Pas d'interactivité htmx nécessaire — c'est une page statique, un instantané
rendu côté serveur.

### Navigation

Ajouter un simple lien "Stats" dans le `<header>` de `index.html` (à côté de la marque
`glane`), pointant vers `/stats`. Ajouter un lien symétrique "← Recherche" vers `/`
en haut de `stats.html`.

## Tests

- `internal/store/stats_test.go` : semer quelques items répartis sur plusieurs
  sources avec des `fetch_status`/`article_summary`/embeddings/tags/sync_state
  variés, vérifier que `Stats()` retourne les agrégats attendus. Couvrir le cas
  base vide (DB vide → tout à zéro, pas d'erreur).
- `internal/web/web_test.go` : ajouter un cas qui appelle `GET /stats` sur un store
  peuplé, vérifier un code 200 et la présence des chiffres clés dans le corps.
- Pas de nouveau fichier de test pour `cmdStats` dans `main.go` (convention
  existante : les fonctions `cmd*` de `main.go` ne sont pas testées unitairement
  directement ; ce sont de fines enveloppes autour de `store`/`internal/*` qui ont
  déjà leurs propres tests). Une vérification manuelle
  (`go build && ./glane stats` / `./glane stats -json`) pendant l'implémentation
  suffit, comme pour les autres fonctions `cmd*` de ce dépôt.

## Hors périmètre

- Pas de données historiques/de tendance (ex. items ajoutés par jour) — c'est un
  instantané à un instant T uniquement.
- Pas de nouveaux flags CLI au-delà de `-json` (pas de filtre `-source`, etc.) —
  YAGNI tant que ce n'est pas demandé.
- Pas de changement de schéma sur `sync_state` pour suivre les compteurs par source
  de façon incrémentale — calculé à la lecture, ce qui reste peu coûteux au vu des
  volumes attendus (archive mono-utilisateur, milliers et non millions de lignes).
