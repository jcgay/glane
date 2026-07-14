# glane — `update` command + scheduled sync

**Date :** 2026-07-14
**Statut :** design validé

## Problème

Tenir sa veille à jour demande trois commandes dans le bon ordre (`sync all`,
`enrich`, `summarize`), lancées à la main. Il n'y a ni commande unique ni entrée
planifiée documentée pour que glane se mette à jour tout seul.

## Objectif

1. Une commande `glane update` qui enchaîne le pipeline complet.
2. Une doc « Scheduling » (launchd macOS + cron) pour la lancer périodiquement,
   en gérant le piège des variables d'environnement en contexte planifié.

## Principes directeurs

- **Ordre de dépendance** : `sync all` (récupère les items) → `enrich` (extrait
  le texte des liens) → `summarize` (résume/tague les items enrichis).
- **Skip du non-configuré**, comme `sync all` : un connecteur sans creds est
  sauté ; `summarize` est sauté si `GLANE_SUMMARY_URL` est absent (pas d'erreur
  fatale — `update` fait la partie configurée).
- **Code de sortie pour le monitoring** : `update` sort non-zéro si une phase a
  échoué (comme `sync all`), pour qu'un job planifié puisse alerter — mais une
  phase qui échoue n'empêche PAS les suivantes de tourner.
- **Drain en une passe** : `enrich`/`summarize` traitent tout le backlog en un
  seul appel à grande limite (`drainLimit = 100000`, soit « tout » à échelle
  perso), PAS en bouclant — ce qui évite tout risque de re-tenter à l'infini un
  item de résumé qui échoue systématiquement. Au-delà de `drainLimit`, le reste
  attend le prochain run.
- **Pas de nouvelle logique** : `update` réutilise le sync-all, `enrich.Run`,
  `summarize` existants ; progression sur stderr via les callbacks existants.

## Périmètre

**Inclus :** commande `glane update` ; refactor du cœur de `sync all` en fonction
retournant un booléen d'échec (au lieu d'`os.Exit`) ; boucles de drain ; section
« Scheduling » du README (plist launchd + cron).

**Exclu (YAGNI) :** daemon résident / scheduler interne (on délègue à
launchd/cron) ; retry avec backoff ; marqueur `summarize_failed` (le drain gère
la terminaison sans lui) ; `--quiet` (séparé).

## `glane update` (`main.go`)

Nouvelle sous-commande `update` (aucun flag pour v1). Déroulé :

1. **sync all** — appelle le cœur partagé (voir refactor) ; note si un connecteur
   a échoué.
2. **enrich (drain, une passe)** — `enrich.Run(s, hc, emb, drainLimit, progress)`
   en un seul appel : `PendingEnrichment(drainLimit)` récupère tout le backlog et
   chaque item passe `fetch_status` à `ok`/`failed`. Une erreur d'`enrich.Run`
   note la phase en échec ; on continue vers summarize.
3. **summarize (drain, une passe)** — si `summarize.FromEnv() == nil` → sauté
   (noté). Sinon `PendingSummary(drainLimit)` puis on résume chaque item **une
   seule fois** (pas de re-boucle) : les items dont le LLM échoue restent
   `article_summary = ''` et sont retentés au prochain run planifié, pas dans
   celui-ci — ce qui évite toute boucle infinie sur un item définitivement en
   échec.
4. En fin : si une phase a signalé un échec → `os.Exit(1)`, sinon 0.

Affichage : réutilise `stderrProgress` (progression par flux/item sur stderr) ;
lignes de résumé de chaque phase sur stdout.

### Refactor du cœur de `sync all`

Aujourd'hui `cmdSyncAll` appelle `os.Exit(1)` directement en cas d'échec — ce qui
tuerait `update` avant `enrich`/`summarize`. Extraire la logique en
`func syncAll(s *store.Store) (failed bool)` (lance chaque connecteur configuré,
log les erreurs sur stderr, saute les non-configurés, imprime la ligne de résumé,
renvoie `failed`). `cmdSyncAll` devient : `if syncAll(s) { os.Exit(1) }`.
`cmdUpdate` appelle `syncAll(s)` et agrège `failed` sans sortir immédiatement.

## Documentation « Scheduling » (README)

Nouvelle section couvrant les deux ordonnanceurs, avec le rappel du piège :
**un job planifié tourne avec un environnement nu** (pas de profil shell) → il
faut fournir explicitement les variables (tokens + `GLANE_DB`).

- **macOS (launchd)** — un plist d'exemple :
  - `Label` (ex. `com.glane.update`),
  - `ProgramArguments` = `[<chemin absolu de glane>, update]`,
  - `EnvironmentVariables` = dict avec `GLANE_DB`, `GITHUB_TOKEN`,
    `MASTODON_INSTANCE_URL`, `MASTODON_ACCESS_TOKEN`, `BLUESKY_HANDLE`,
    `BLUESKY_APP_PASSWORD`, `GLANE_EMBED_URL`/`MODEL`/`KEY`,
    `GLANE_SUMMARY_URL`/`MODEL`/`KEY` (ceux qu'on utilise),
  - `StartCalendarInterval` (ex. tous les jours à 7h),
  - `StandardOutPath` / `StandardErrorPath` (logs).
  - Chargement : `launchctl load ~/Library/LaunchAgents/com.glane.update.plist`
    (et `launchctl unload` pour retirer).
- **cron (Linux/générique)** — variables en tête de crontab + une ligne :
  `0 7 * * * /usr/local/bin/glane update >> ~/.local/state/glane/update.log 2>&1`.

Insister : chemin ABSOLU de `glane` (pas de PATH garanti), et le fichier
SQLite (`GLANE_DB`) doit pointer vers un chemin absolu stable.

## Gestion des erreurs

- Chaque phase isolée : une erreur est loguée sur stderr, n'interrompt pas les
  autres phases, et fait sortir `update` en non-zéro à la fin.
- `enrich`/`summarize` restent idempotents et reprenables (curseurs / colonnes
  d'état) — un `update` interrompu se rattrape au suivant.

## Tests

`go test`, httptest, **aucun accès réseau réel** :

- **Refactor sync-all** : `syncAll` renvoie `true` si un connecteur configuré
  échoue, `false` si tout est sauté/OK — testable en pilotant les variables
  d'env dans le test (ou en gardant la couverture existante de `sync all` via la
  vérif manuelle « tout non-configuré → exit 0 »).
- **Drain enrich** : httptest servant N>batch items enrichissables ; après
  `update` (ou une fonction `enrichAll` testable), 0 item ne reste en
  `fetch_status = ''`.
- **Drain summarize** : un item dont l'endpoint LLM échoue toujours ne fait PAS
  boucler à l'infini (la boucle s'arrête sur `done == 0`) ; un mélange
  succès/échec résume les succès et laisse les échecs pending.
- La surface CLI : `glane update` existe et est listée dans l'usage.

Note d'implémentation : pour rendre le drain testable sans passer par
`os.Exit`, extraire les boucles en fonctions (`enrichAll`, `summarizeAll` ou
équivalent) qui renvoient (compte, failed) ; `cmdUpdate` les orchestre et gère le
code de sortie.

Pas de nouvelle dépendance — stdlib uniquement.

## Ordre de construction

1. Refactor `syncAll` (cœur non-exitant) + `enrichAll`/`summarizeAll` (drain,
   testables) + `cmdUpdate` (orchestration, skip non-configuré, exit code) +
   sous-commande `update` dans le dispatch + usage + tests.
2. README : section « Scheduling » (plist launchd + cron + piège env) et ajout de
   `glane update` à la liste des commandes.
