# glane — LLM summaries + free-tag categorization

**Date :** 2026-07-14
**Statut :** design validé

## Problème

`glane` extrait déjà le texte des articles liés (`enrich`) et les rend
cherchables. Mais quand on retombe sur un résultat, un mur de texte brut ne dit
pas d'un coup d'œil « c'est quoi, ça » — et rien ne permet de parcourir sa veille
par sujet (« tous mes bookmarks Kubernetes »). C'est exactement le point de
douleur : on sauvegarde et on oublie.

## Objectif

Un LLM optionnel qui, par un **seul appel par article**, produit à la fois un
**résumé lisible** et des **tags de sujet libres**, pour :
- reconnaître un résultat oublié d'un coup d'œil (le résumé) ;
- parcourir et filtrer la veille par thème (les tags).

## Principes directeurs

- **Optionnel, dégradation gracieuse** : sans `GLANE_SUMMARY_URL`, `glane`
  fonctionne comme aujourd'hui. Le résumé/tags sont une couche bonus.
- **Un seul appel LLM par article** : résumé + tags dans une réponse JSON
  structurée, pas deux requêtes.
- **Commande séparée `glane summarize`**, pas dans `enrich` : backfille le corpus
  déjà enrichi sans re-fetcher, pilotage coût/rythme du LLM indépendant.
- **Reprenable et idempotent** : un item sans résumé est repris au run suivant ;
  une erreur par item est loguée et sautée, jamais fatale pour le lot.
- **Pas de migration douloureuse** : les tags vont dans une nouvelle table
  (`CREATE TABLE IF NOT EXISTS`), donc aucun rebuild de l'index FTS existant.

### Décision explicite (rediscutée pendant le design)

Le résumé **ne remplace PAS** le texte d'article dans la recherche : `article_text`
reste indexé en plein-texte intégralement (un résumé perd des mots-clés). Le
résumé est *additif* (`article_summary` est déjà dans l'index FTS) et sert surtout
d'**extrait lisible**. On **ne ré-embed PAS** à partir du résumé : le vecteur
actuel (`titre + début du texte`) et un vecteur « titre + résumé » sont deux
projections lossy différentes, sans gain clair (et régression sur les articles
courts). Les embeddings restent inchangés.

## Périmètre

**Inclus :** `internal/summarize` (client chat) ; `glane summarize [--limit N]` ;
table `item_tags` + helpers store ; `glane tags` (liste + comptes) ; filtre
`--tag` sur `search` (fil des deux classements) ; affichage résumé + tags (web + CLI).

**Exclu (YAGNI) :** taxonomie fixe / hybride (tags libres retenus) ; ré-embedding
depuis le résumé ; résumé intégré à `enrich` ; normalisation/alias des tags (on
observe la dérive via `glane tags`, on corrigera si ça gêne).

## Configuration

Trio dédié (comme les embeddings), endpoint **chat/completions** compatible OpenAI :

| variable | rôle |
|---|---|
| `GLANE_SUMMARY_URL` | base URL du LLM de résumé ; absente → summarize désactivé (fatal si on lance `summarize`) |
| `GLANE_SUMMARY_MODEL` | nom du modèle |
| `GLANE_SUMMARY_KEY` | clé API (`Authorization: Bearer …`) ; omise pour un endpoint local |

## Client (`internal/summarize`)

`FromEnv() *Client` (nil si `GLANE_SUMMARY_URL` absente).

`Summarize(ctx, title, articleText string) (Result, error)` où
`Result { Summary string; Tags []string }`.

- `POST {URL}/chat/completions`, corps `{model, messages:[{role:"system",...},
  {role:"user", content: title + "\n\n" + article}]}`.
- **System prompt** (fixe) : demande une réponse **UNIQUEMENT** en JSON :
  `{"summary": "<2-3 phrases pour un lecteur technique, l'idée clé>", "tags":
  ["<3-6 tags de sujet/techno en minuscules>"]}`.
- Parse `choices[0].message.content`. **Lenient** : si le contenu n'est pas du
  JSON pur (fences ```json, prose autour), extraire le premier objet `{...}`
  (première `{` → dernière `}`) avant `json.Unmarshal`.
- Post-traitement des tags : trim, minuscules, dédup, on retire les vides, on
  plafonne à 6.
- L'`articleText` est tronqué de façon rune-safe (~8000 caractères) avant envoi
  pour borner le coût.
- Statut non-200 → erreur (avec le code) ; JSON illisible/sans summary → erreur.

## Store

- **`article_summary`** : colonne existante (déjà dans l'index FTS) — aucun
  changement de schéma pour le résumé.
- **Nouvelle table** :
  ```sql
  CREATE TABLE IF NOT EXISTS item_tags (
    item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    PRIMARY KEY (item_id, tag)
  );
  CREATE INDEX IF NOT EXISTS item_tags_tag ON item_tags(tag);
  ```
  (`ON DELETE CASCADE` actif — les foreign keys sont déjà activées via le DSN.)
- `PendingSummary(limit int) ([]Item, error)` : `article_text != '' AND
  article_summary = '' AND fetch_status = 'ok'` (défaut limit 100).
- `SaveSummary(id int64, summary string, tags []string) error` : dans une
  transaction — `UPDATE items SET article_summary=?` (déclenche le trigger FTS) ;
  `DELETE FROM item_tags WHERE item_id=?` ; `INSERT` chaque tag.
- `TagCounts() ([]TagCount, error)` où `TagCount{Tag string; Count int}` :
  `SELECT tag, COUNT(*) c FROM item_tags GROUP BY tag ORDER BY c DESC, tag`.
- `ByTag(tag string, f Filter) ([]Result, error)` : items ayant ce tag, triés
  `created_at DESC`, filtrés par `f.Source`/`f.Since`, cappés par `f.Limit`
  (pour le parcours `search --tag X` sans requête texte).
- **`Filter.Tag string`** (nouveau champ) : quand présent,
  - `SearchFTS` ajoute `AND EXISTS (SELECT 1 FROM item_tags t WHERE
    t.item_id=i.id AND t.tag=?)` ;
  - `AllEmbeddings(model, f)` ajoute la même contrainte via un `EXISTS`, pour que
    les candidats sémantiques respectent aussi le tag (cohérent avec le
    traitement actuel de `--source`/`--since` ; évite la fuite en mode hybride).

## CLI & UI

- **`glane summarize [--limit N]`** : `summarize.FromEnv()` nil → fatal (`set
  GLANE_SUMMARY_URL to generate summaries`). Sinon, boucle sur `PendingSummary` ;
  pour chaque item appelle `Summarize(title, article_text)`, `SaveSummary`.
  Erreur/parse par item → log stderr + continue. Affiche `summarized N items (M
  failed)`.
- **`glane tags`** : imprime la liste `tag  count`, triée par count décroissant.
- **`search --tag X`** : nouveau flag. Résolution dans `cmdSearch` —
  - requête texte + `--tag` → `search.Hybrid` avec `Filter.Tag` (fil FTS +
    sémantique) ;
  - `--tag` seul (requête vide) → `store.ByTag` ;
  - ni l'un ni l'autre → erreur d'usage.
- **Affichage** : `results.html` et la ligne CLI montrent `article_summary` comme
  extrait quand présent (sinon le texte du post), plus les tags de l'item en
  petites étiquettes. (La ligne CLI ajoute les tags entre crochets ; le template
  web une petite liste.) Récupérer les tags d'un item de résultat : soit les
  charger dans `GetItems`/résultats via une jointure, soit un `TagsFor(ids)` en
  lot — au choix de l'implémentation, mais les tags affichés doivent venir du
  store, pas être re-devinés.

## Gestion des erreurs

- Config absente → `fatal` nommant `GLANE_SUMMARY_URL`.
- Endpoint injoignable / non-200 / JSON invalide → erreur par item, loguée,
  l'item reste sans résumé et sera repris (idempotent).
- `summarize` sans aucun item en attente → `summarized 0 items`, exit 0.

## Tests

`go test`, httptest, **aucun accès réseau réel** :

- **Client** : serveur httptest renvoyant un contenu chat ; vérifier le parse de
  `{summary, tags}`, y compris un contenu **entouré de fences/prose** (extraction
  lenient), et le nettoyage des tags (minuscules/dedup/cap). Non-200 → erreur.
- **Store** : `SaveSummary` puis `PendingSummary` (l'item n'est plus en attente) ;
  le résumé devient cherchable via `SearchFTS` ; `TagCounts` agrège ; `ByTag`
  renvoie les bons items ; `Filter.Tag` restreint `SearchFTS` ET
  `AllEmbeddings` (pas de fuite hybride) — test dédié avec deux items de tags
  différents.
- **Commande** : la boucle `summarize` continue après un item dont l'endpoint
  renvoie une erreur (1 ok, 1 échec → `summarized 1 items (1 failed)`), via un
  serveur httptest et un `summarize.Client` pointé dessus.

Pas de nouvelle dépendance — stdlib (`net/http`, `encoding/json`, `strings`,
`unicode/utf8`) uniquement.

## Ordre de construction

1. `internal/summarize` : client chat + parse JSON lenient + nettoyage tags + tests.
2. Store : table `item_tags`, `PendingSummary`, `SaveSummary`, `TagCounts`,
   `ByTag`, champ `Filter.Tag` (fil dans `SearchFTS` + `AllEmbeddings`) + tests.
3. CLI/UI : `summarize`, `tags`, flag `--tag` (Hybrid vs ByTag), affichage résumé
   + tags (web + CLI) + manual checks.
