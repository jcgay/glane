# Navigation par tags dans l'interface web

## Problème

L'interface web permet de filtrer par tag, mais seulement en **réactif** :
il faut d'abord lancer une recherche texte, obtenir un résultat, puis cliquer
sur un des tags affichés (`/search?tag=X`, déjà fonctionnel via `store.ByTag`).

Il manque la **découverte** : voir les tags disponibles *avant* toute
recherche, pour parcourir la veille par thème depuis la page d'accueil.

## Objectif

Afficher la liste des tags sur la page d'accueil. Un clic sur un tag lance la
recherche par tag existante.

## Ce qui existe déjà (à réutiliser, ne rien réécrire)

- `store.TagCounts() ([]TagCount, error)` — tags + compteur, triés par
  fréquence décroissante. **Non utilisé côté web aujourd'hui.**
- `GET /search?tag=X` — rend `results.html`, cible `#results` (htmx).
- Style `.tag` (chip cliquable) dans `results.html`.
- CSS : `#results:not(:empty) + .idle { display: none }` — la zone idle se
  masque automatiquement dès qu'une recherche remplit `#results`.

## Conception

**Rendu serveur au chargement de la page.** Aucun nouvel endpoint, aucun JS,
aucune dépendance.

1. **`internal/web/web.go`** — handler `/` : passer `s.TagCounts()` au template
   au lieu de `nil`. Sur erreur, dégrader silencieusement (rendre la page sans
   tags), cohérent avec la philosophie du projet.
2. **`templates/index.html`** — dans la zone `.idle`, sous le message d'invite
   existant, ajouter un titre « Parcourir par tag » puis les chips. Chaque chip :
   `<a hx-get="/search?tag={{.Tag | urlquery}}" hx-target="#results"
   hx-indicator=".bar">{{.Tag}} ·{{.Count}}</a>`.
   Style repris de `.tag` (préfixe `#`, badge compteur).

Le message d'invite « Tapez pour chercher… » est **conservé**, les tags
viennent en dessous.

## Comportement

- Accueil (aucune recherche) : invite + liste des tags visibles.
- Clic sur un tag → `#results` se remplit → la zone idle (invite + tags) se
  masque via le CSS existant.
- Effacer la recherche → `#results` se vide → tags de nouveau visibles.

## Décisions (YAGNI)

- Tous les tags affichés, triés par fréquence (ordre SQL existant). Pas de
  pagination ni « voir plus » — à ajouter seulement si la liste devient trop
  longue en usage réel.
- Chips plats avec compteur, **pas** de nuage pondéré (taille variable) :
  plus lisible, aucun calcul.
- Aucun état persistant / filtre combiné : hors périmètre (l'utilisateur a
  choisi « découverte » uniquement).

## Périmètre

Touche : `internal/web/web.go` (~1 ligne), `templates/index.html` (un bloc +
un peu de CSS). Pas de changement de schéma, de CLI, ni d'env var → pas de
mise à jour README nécessaire.

## Test

Étendre `web_test.go` : un item avec tags → `GET /` contient le nom du tag et
un lien `hx-get="/search?tag=..."`.
