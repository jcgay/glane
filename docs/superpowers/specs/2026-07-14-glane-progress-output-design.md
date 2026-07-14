# glane — progress output for sync / enrich / summarize

**Date :** 2026-07-14
**Statut :** design validé

## Problème

Les trois commandes à boucle longue (`sync`, `enrich`, `summarize`) sont
silencieuses : rien ne s'affiche pendant l'exécution, seulement une ligne de
résumé à la fin. Sur un `sync` qui pagine beaucoup, un `enrich` qui fait des
fetch réseau lents ou un `summarize` qui enchaîne des appels LLM, on ne sait pas
ce qui se passe ni où on en est.

## Objectif

Afficher un état d'avancement en cours d'exécution pour `sync`, `enrich` et
`summarize`, sous forme de lignes défilantes sur stderr.

## Principes directeurs

- **Callback optionnel, churn minimal** : un `progress func(string)` variadique
  optionnel (`progress ...func(string)`) sur les fonctions à boucle interne, pour
  que TOUS les appels de test existants compilent sans modification (absent =
  silencieux).
- **stderr pour la progression, stdout pour le résumé** : les lignes d'avancement
  vont sur stderr ; les lignes de résumé finales existantes restent sur stdout,
  intactes (pipe/cron propres).
- **Lignes défilantes, pas de TTY magique** : une ligne par étape/page ; marche
  identiquement en interactif, en pipe et en cron. Pas de `\r`, pas de détection
  de terminal.
- **Aucun changement de logique** : la pagination, les curseurs, les résumés
  finaux sont inchangés ; on ajoute seulement des appels `report(...)`.

## Périmètre

**Inclus :** callback de progression sur `github.Sync`, `mastodon.Sync`,
`bluesky.Sync`, `enrich.Run` ; émission directe dans `cmdSummarize` ; câblage
d'un callback stderr dans `main.go` pour `sync`/`sync all`, `enrich`, `summarize`.

**Exclu (YAGNI) :** compteur live réécrit en place / spinner / barre de
progression TTY ; pourcentages/ETA (les totaux ne sont pas connus d'avance en
pagination) ; progression sur `import twitter` (rapide, local, déjà un résumé) ;
verbosité configurable / `--quiet`.

## Mécanisme

Type de callback : `func(string)`. Convention : un helper interne
`report(msg)` qui n'appelle le callback que s'il est fourni et non-nil :

```go
func firstProgress(progress []func(string)) func(string) {
	if len(progress) > 0 && progress[0] != nil {
		return progress[0]
	}
	return func(string) {} // no-op
}
```

Chaque fonction concernée accepte `progress ...func(string)` en dernier
paramètre et construit `report := firstProgress(progress)` en tête.

`main.go` définit un callback unique qui écrit sur stderr :
```go
func stderrProgress(msg string) { fmt.Fprintln(os.Stderr, msg) }
```
et le passe aux appels lancés par `cmdSync`/`cmdSyncAll`/`cmdEnrich`. Pour
`summarize`, `cmdSummarize` appelle directement `stderrProgress` dans sa boucle.

## Contenu et format des messages

- **sync (par flux, par page — total cumulé du flux)** : `"<source>: <label>… <n>"`.
  - GitHub : `github: stars… 200`.
  - Mastodon : `mastodon: favourites… 40`, `mastodon: bookmarks… 12`,
    `mastodon: my posts… 8`.
  - Bluesky : `bluesky: likes… 100`, `bluesky: saved… 30`,
    `bluesky: my posts… 15`.
  Le message est émis après chaque page récupérée, avec le total cumulé du flux
  jusque-là. Le label du flux est fourni par le connecteur (via le champ label du
  stream / un paramètre à `syncStream`).
- **enrich (par item)** : `"enrich [<i>/<n>] <host>…"` où `n` = nombre d'items en
  attente ce run, `i` = index courant (1-based), `host` = hôte du lien traité.
  Le compteur d'échecs reste dans le résumé final (`enriched N, failed M`).
- **summarize (par item)** : `"summarize [<i>/<n>]…"` où `n` = items en attente,
  `i` = index courant.

`sync all` : aucun préfixe supplémentaire nécessaire — chaque message porte déjà
son `<source>`. La ligne de résumé agrégée finale (stdout) est inchangée.

## Signatures modifiées

- `func (…) github.Sync(s, token, hc, progress ...func(string)) (int, error)`
- `func (…) mastodon.Sync(s, baseURL, token, hc, progress ...func(string)) (int, error)`
- `func (…) bluesky.Sync(s, handle, appPassword, hc, progress ...func(string)) (int, error)`
- `func (…) enrich.Run(s, hc, emb, limit, progress ...func(string)) (int, int, error)`

Les boucles internes partagées (`syncStream` de chaque connecteur) reçoivent le
`report` (et un label de flux) pour émettre par page. `verify_credentials`
(Mastodon) n'émet rien.

`cmdSummarize` (main.go) émet directement via `stderrProgress` dans sa boucle
existante (pas de changement de signature côté `summarize.Client`).

## Choix délibérés

- Progression sur **stderr** ; résumés finaux sur **stdout** (inchangés).
- Émission **par page** pour sync (pas par item — une page = jusqu'à 40–100
  items, granularité suffisante et non spammy), **par item** pour enrich/summarize
  (opérations lentes unitaires, où le feedback par item est ce qu'on veut).
- Pas de pourcentage : les totaux ne sont pas connus avant la fin de la
  pagination (sync). Pour enrich/summarize, `n` = taille du lot en attente
  (connu), donc `[i/n]` est exact.

## Gestion des erreurs

Inchangée. La progression est purement informative ; un callback ne renvoie pas
d'erreur et ne modifie jamais le flux de contrôle. Un callback absent/nil →
no-op silencieux.

## Tests

`go test`, **aucun accès réseau réel** (httptest, comme l'existant) :

- **Callback reçoit des messages** : pour au moins un connecteur (ex. Mastodon
  ou Bluesky) et pour `enrich.Run`, passer un callback qui accumule les messages
  dans un slice et vérifier qu'il est non-vide et contient le(s) label(s)
  attendu(s) (ex. `favourites`, ou `enrich [`).
- **Silencieux sans callback** : les tests existants (qui n'en passent pas)
  restent inchangés et verts → prouve la rétro-compatibilité variadique.

Pas de nouvelle dépendance — stdlib (`fmt`, `os`) uniquement.

## Ordre de construction

1. Connecteurs : ajouter `progress ...func(string)` + `report` + appels par page
   dans `github`, `mastodon`, `bluesky` (via leurs `syncStream` respectifs, avec
   label de flux) + un test « callback reçoit des messages ».
2. `enrich.Run` : `progress ...func(string)` + report par item (`[i/n] host`) +
   test.
3. `main.go` : `stderrProgress` ; le câbler dans `cmdSync`/`cmdSyncAll`/`cmdEnrich`
   et l'émettre dans la boucle de `cmdSummarize` ; vérif manuelle de la surface.
