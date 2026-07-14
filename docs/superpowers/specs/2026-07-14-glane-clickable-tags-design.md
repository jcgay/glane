# glane — clickable tags in the web UI

**Date :** 2026-07-14
**Statut :** design validé

## Problème

L'UI web affiche les tags de chaque résultat (`results.html`) mais ils ne sont
pas cliquables, et le handler `/search` ne lit pas de paramètre `tag`. Côté
serveur tout existe déjà (`store.ByTag`, `Filter.Tag`, `search.Hybrid`) et le CLI
a `--tag` — il ne manque que le câblage front.

## Objectif

Rendre les tags cliquables dans l'UI web : cliquer un tag affiche tout ce qui
porte ce tag (parcours par sujet), en réutilisant la logique serveur existante.

## Principes directeurs

- **Réutiliser le serveur** : le handler délègue à `store.ByTag` / `search.Hybrid`
  avec `Filter.Tag`, comme le CLI. Aucune nouvelle logique de recherche.
- **htmx, pas de JS custom** : un tag est un lien htmx (`hx-get`) ciblant
  `#results`, comme le reste de l'UI.
- **Échappement correct** : la valeur du tag est URL-encodée dans le lien
  (`{{. | urlquery}}`) — `hx-get` n'est pas un attribut URL reconnu par
  `html/template`, donc l'encodage query doit être explicite (sinon un tag comme
  `c++`/`c#` casserait le paramètre).

## Périmètre

**Inclus :** handler `/search` lit `tag` + route parcours-par-tag ; tags rendus
cliquables dans `results.html` ; style « cliquable » ; tests handler.

**Exclu (YAGNI) :** nuage/liste de tags dédié (page « tous mes tags ») ;
indicateur « filtre actif » avec bouton ✕ ; combiner le tag cliqué avec le texte
courant de la barre de recherche (cliquer un tag = parcours pur du tag).

## Handler `/search` (`internal/web/web.go`)

Lire `q`, `source` **et** `tag`. Routage (miroir de `cmdSearch`) :

- `q == "" && tag == ""` → réponse vide (comportement actuel).
- `q == "" && tag != ""` → `store.ByTag(tag, store.Filter{Limit: 50})` — parcours
  de tout ce qui porte ce tag, du plus récent au plus ancien.
- `q != ""` → `search.Hybrid(s, gembed.FromEnv(), q, store.Filter{Source: source,
  Tag: tag, Limit: 50})` — recherche texte, filtrée par tag (et source) si
  présents.

Puis `s.AttachTags(res)` (inchangé) et rendu de `results.html` (inchangé). En cas
d'erreur : 500, comme aujourd'hui.

## Template `results.html`

Remplacer chaque `<span class="tag">{{.}}</span>` par un lien htmx :

```html
<a class="tag" href="#" hx-get="/search?tag={{. | urlquery}}" hx-target="#results">{{.}}</a>
```

- `hx-target="#results"` : recharge la zone de résultats (comme la barre de
  recherche).
- `{{. | urlquery}}` : encodage query explicite de la valeur du tag.
- `href="#"` : lien inerte (htmx gère le clic) ; le tag reste auto-échappé HTML
  par `html/template` pour l'affichage.

## Style (`index.html`)

`.tag` devient visiblement cliquable : `cursor: pointer` + un état `:hover`
(léger changement de fond). Aucun autre changement de layout.

## Comportement

Cliquer un tag = « montre-moi tout ce qui porte ce tag » (équivaut à
`glane search --tag X` sans requête). Les résultats affichent eux-mêmes leurs
tags cliquables → on pivote de sujet en sujet en cliquant. La barre de recherche
texte reste indépendante ; un tag cliqué ne mélange pas le texte courant (v1).

## Gestion des erreurs

Inchangée : erreurs store/recherche → 500 ; tag inexistant → 0 résultat (le
template affiche « Aucun résultat. »).

## Tests

`go test`, httptest via `handler(s)` (pas de vrai réseau) :

- **Parcours par tag** : seeder deux items enrichis dans des sujets différents,
  `SaveSummary` avec des tags distincts (ex. `rust` vs `go`). `GET /search?tag=rust`
  → le fragment contient l'item `rust` et PAS l'item `go`.
- **Texte + tag** : `GET /search?q=<mot>&tag=rust` → filtré à la fois par le texte
  et le tag (l'item `go`, même s'il matche le texte, est exclu).
- **Tag vide + q vide** → réponse vide (inchangé).
- **Rendu cliquable** : le fragment d'un résultat tagué contient
  `hx-get="/search?tag=rust"` (et pour un tag à caractères spéciaux, la forme
  URL-encodée), prouvant que le lien htmx est bien généré.

Pas de nouvelle dépendance — stdlib + htmx déjà embarqué.

## Ordre de construction

1. `internal/web/web.go` : lire `tag`, router (`ByTag` vs `Hybrid` avec
   `Filter.Tag`) ; `internal/web/templates/results.html` : tags cliquables ;
   `internal/web/templates/index.html` : style `.tag` cliquable ; tests handler.
