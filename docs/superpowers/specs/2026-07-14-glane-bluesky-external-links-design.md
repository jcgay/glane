# glane — capture Bluesky external links for enrichment

**Date :** 2026-07-14
**Statut :** design validé

## Problème

Les items Bluesky ne stockent que `record.text`. Or les liens externes (le
lien d'article qu'un post partage) ne sont PAS dans `text` : ils vivent dans
`record.embed` (une carte de lien). Résultat : un post liké/sauvegardé/reposté
qui pointe vers un article ne s'enrichit pas — `enrich` fait `FirstURL(it.Text)`,
ne trouve aucun lien, retombe sur `it.URL` = le permalien `bsky.app` (une SPA
que readability ne sait pas extraire). Côté Mastodon ça marche déjà (l'URL finit
dans le texte via `stripHTML`) ; Bluesky a ce trou.

## Objectif

Capturer le lien externe d'un post Bluesky et le rendre disponible pour
l'enrichissement et la recherche, sans changement de schéma ni d'`enrich`.

## Principes directeurs

- **Réutiliser le mécanisme existant** : mettre l'URL externe (et son
  titre/description) dans le `Text` de l'item, exactement comme Mastodon. Alors
  `enrich`'s `FirstURL(it.Text)` la trouve tout seul, et FTS indexe le contexte.
- **Un seul point** : la modif est dans le `toItem` partagé de `postView`, donc
  les QUATRE flux Bluesky (likes, saved, own, reposts) en bénéficient d'un coup.
- **Aucun changement de schéma / d'`enrich` / de store**. `it.URL` reste le
  permalien `bsky.app` (le lien « voir le post original »).

## Périmètre

**Inclus :** décodage de `post.embed` dans `internal/bluesky`, extraction du lien
externe (deux formes de vue), ajout titre+description+uri au `Text`.

**Exclu (YAGNI) :** embeds images/vidéo (pas de lien externe) ; quote-post sans
média externe ; télécharger la miniature (`thumb`) ; tout changement à `enrich`
ou au store.

## API vérifiée (lexicons atproto, 2026-07-14)

Dans un `postView`, `embed` est une union discriminée par `$type` :

- `app.bsky.embed.external#view` → `embed.external.{uri, title, description}`
  (carte de lien — le cas principal).
- `app.bsky.embed.recordWithMedia#view` → `embed.media` est elle-même une union ;
  si `embed.media.$type == "app.bsky.embed.external#view"`, alors
  `embed.media.external.{uri, title, description}` (quote-post AVEC un lien).
- Autres (`images#view`, `video#view`, `record#view` seul) → pas de lien externe.

## Implémentation (`internal/bluesky/bluesky.go`)

Étendre la struct `postView` pour décoder `embed` :

```go
type externalView struct {
	URI         string `json:"uri"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type embedView struct {
	Type     string        `json:"$type"`
	External *externalView `json:"external"` // for app.bsky.embed.external#view
	Media    *struct {
		Type     string        `json:"$type"`
		External *externalView `json:"external"`
	} `json:"media"` // for app.bsky.embed.recordWithMedia#view
}
```

`postView` gagne `Embed *embedView `json:"embed"``.

Helper `externalLink(e *embedView) *externalView` :
- `e == nil` → nil.
- `e.Type == "app.bsky.embed.external#view" && e.External != nil` → `e.External`.
- `e.Type == "app.bsky.embed.recordWithMedia#view" && e.Media != nil &&
  e.Media.Type == "app.bsky.embed.external#view" && e.Media.External != nil` →
  `e.Media.External`.
- sinon nil.

Dans `toItem`, construire le texte : partir de `record.text`, et si un lien
externe est présent, y ajouter (séparés par des espaces) `title`, `description`,
puis `uri` :

```go
text := p.Record.Text
if ext := externalLink(p.Embed); ext != nil {
	parts := []string{text, ext.Title, ext.Description, ext.URI}
	// join non-empty parts with a single space
	text = strings.TrimSpace(strings.Join(nonEmpty(parts), " "))
}
```
(`nonEmpty` : petit helper filtrant les chaînes vides pour éviter les espaces
multiples ; ou `strings.Join` puis collapse — au choix de l'implémentation,
tant que l'URI apparaît telle quelle dans le texte pour que `FirstURL` la
trouve.)

L'URI doit apparaître **littéralement** (non tronquée) dans `Text` pour que
`enrich.FirstURL` la capte. `Text` est bien ce qui est indexé en FTS (titre +
description deviennent cherchables) et ce que `enrich` scanne.

## Effet

- `enrich` (branche non-github → `FirstURL(it.Text)`) trouve désormais l'URL de
  l'article et va le chercher, au lieu de retomber sur le permalien `bsky.app`.
- Titre + description de l'article sont cherchables tout de suite (avant même
  `enrich`).
- Vaut pour les 4 flux Bluesky via le `toItem` partagé.

## Gestion des erreurs

Aucune nouvelle. Embed absent / autre type / champs manquants → aucun lien
ajouté, `Text` = `record.text` seul (comportement actuel). Décodage tolérant
(pointeurs nil).

## Tests

`go test`, httptest/unit, **aucun accès réseau réel** :

- **Unit `externalLink`/`toItem`** :
  - `external#view` → `Text` contient l'URI, le titre et la description.
  - `recordWithMedia#view` avec media `external#view` → idem.
  - `images#view` (ou embed absent) → `Text` = `record.text` seul.
- **Bout-en-bout (httptest, via `Sync`)** : un like dont le post porte un embed
  `external#view` → recherche par le titre de l'article (`SearchFTS`) le trouve,
  et l'item stocké a un `Text` qui contient l'URI (donc `enrich` la suivrait).

Pas de nouvelle dépendance — stdlib (`strings`) uniquement.

## Ordre de construction

1. `internal/bluesky` : structs `externalView`/`embedView` + champ `Embed` sur
   `postView` ; helper `externalLink` ; intégration dans `toItem` ; tests unit +
   bout-en-bout.
