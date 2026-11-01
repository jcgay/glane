# glane — application desktop

**Date :** 2026-11-01
**Statut :** design validé

## Problème

L'interface installable (manifest PWA, 15eafa1) n'est vivante que si
`glane serve` tourne. Il faut donc écrire à la main un service launchd ou
systemd, y recopier ses variables d'environnement et le chemin du binaire, et
le serveur tourne en permanence sur le port fixe 8080, même quand on ne s'en
sert pas.

## Objectif

`glane app` : une fenêtre native qui démarre son propre serveur et l'arrête
quand on la ferme, comme l'application Android.

Critère de réussite : un double-clic sur `Glane.app` (ou l'entrée du lanceur
Ubuntu) ouvre la recherche ; fermer la fenêtre ne laisse aucun processus.

## Hypothèses

- macOS et Linux (Ubuntu). Windows hors périmètre, même si glaze le supporte.
- Le build reste sans cgo (`CGO_ENABLED=0`). La décision de 15eafa1 d'écarter
  une webview reposait sur cgo ; glaze lève cette objection en chargeant le
  WebKit du système au runtime via purego.

## Hors périmètre

- `--port`, `--read-only`, le jeton : restent propres à `glane serve`.
- Verrou d'instance unique : deux lancements donnent deux fenêtres et deux
  serveurs sur la même base, ce que SQLite supporte.
- Bundle `.app` ou paquet `.deb` livré par le projet : la doc explique comment
  créer le lanceur, le repo n'en contient pas.
- Signature et notarisation macOS.

## Choix de la webview

[crgimenes/glaze](https://github.com/crgimenes/glaze) (MIT) : port en Go pur de
webview/webview sur [purego](https://github.com/ebitengine/purego). Il lie
WKWebView (macOS) et WebKitGTK (Linux) déjà présents, sans embarquer de
bibliothèque native. Sa seule dépendance est purego.

Alternatives écartées :

- **Chrome en `--app` + heartbeat SSE** : dépend de Chrome, et il faut un
  `/alive` et un délai d'inactivité pour savoir quand arrêter le serveur.
- **Navigateur par défaut** : un onglet, pas une fenêtre ; l'origine change à
  chaque port aléatoire, donc pas de « Add to Dock ».
- **Service généré** (`glane service install`) : garde le port fixe et le
  serveur permanent.

Risque accepté : glaze est jeune (créé en février 2026, v0.0.81) et presque
entièrement écrit par une seule personne. Si le projet meurt, `glane serve` et
la PWA restent disponibles.

### Spike (2026-11-01, macOS Intel)

Un prototype qui affiche la vraie interface sur la base réelle a validé :
build `CGO_ENABLED=0`, recherche et raccourcis clavier, liens
`target=_blank` et `window.open` relayés à `open` par `OnNewWindow`, copie
dans le presse-papiers (`y`), fermeture de fenêtre → `Run()` rend la main.

Deux leçons :

- Sans `runtime.LockOSThread()` dans un `init()` du paquet `main`, AppKit
  s'exécute hors du thread principal et le processus meurt en `SIGILL`.
- Le prototype lançait `glane serve` en processus fils ; au crash, le fils est
  resté orphelin. Le serveur tourne donc dans le même processus que la fenêtre.

Non vérifié : Linux / WebKitGTK.

Compilation croisée : linux, darwin et windows (amd64, arm64) compilent sans
cgo. `GOOS=android` échoue : purego y exige cgo (`undefined: cgo.Dlopen`). Or
l'APK embarque le binaire `glane` compilé pour Android. Le code qui importe
glaze porte donc `//go:build !android`, et un fichier `app_android.go` fournit
un `cmdApp` qui refuse proprement.

## Design

### `internal/web`

- `Handler(s *store.Store) http.Handler` exporte l'actuel `handler`. `Serve`
  ne change pas.
- L'icône `static/icon-512.png`, déjà embarquée, est exportée pour la fenêtre.

### `app.go` (paquet `main`)

`cmdApp(args)` :

1. `store.Open(dbPath())`, `net.Listen("tcp", "127.0.0.1:0")`, puis
   `http.Server{Handler: web.Handler(s)}` servi dans une goroutine.
2. `glaze.SetAppIcon` avec l'icône, pour que le Dock n'affiche pas l'icône
   générique d'un exécutable.
3. `glaze.NewWithOptions` avec `HideUntilLoaded` et `OnNewWindow`, qui ouvre
   l'URL dans le navigateur par défaut (`open` sur macOS, `xdg-open` sinon).
   Titre « glane », 1200×800, puis `Navigate`, `Run`, `Destroy`.
4. Au retour de `Run` : `srv.Shutdown` avec un délai de 5 s, `s.Close()`,
   sortie 0.

Aucun flag. Le message de démarrage indique la base servie, comme `serve`.

`app.go` porte aussi `func init() { runtime.LockOSThread() }`, commenté :
AppKit et GTK exigent le thread principal. Sans effet sur les autres commandes.

### `app_android.go`

`cmdApp` qui sort en erreur : `glane app is not available on Android`.

### `main.go`

Un `case "app"` qui appelle `cmdApp(os.Args[2:])` avant l'ouverture commune de
la base (comme `serve`), et `app` dans la ligne d'usage.

## Erreurs

- Échec d'ouverture de la base ou d'écoute : `fatal`, comme `serve`.
- Échec de `glaze.NewWithOptions` (WebKitGTK absent) : `fatal` avec
  `webview unavailable: <err>; on Debian/Ubuntu: apt install libwebkit2gtk-4.1-0, or use glane serve`.
- Échec de `open`/`xdg-open` pour un lien : message sur stderr, la fenêtre
  reste ouverte.
- Lancé depuis le Finder ou le lanceur, stderr ne va nulle part ; la doc
  montre comment le rediriger vers un fichier.

## Lanceurs (documentation seulement)

Une app lancée hors d'un terminal ne voit pas l'environnement du shell
(`GLANE_DB`, `GLANE_EMBED_*`, `PATH`). Le lanceur passe donc par le shell de
login, qui charge la configuration de l'utilisateur :

- macOS : `~/Applications/Glane.app/Contents/MacOS/glane`, script
  `#!/bin/sh` + `exec "$SHELL" -lc 'glane app'`, avec un `Info.plist`
  minimal (`CFBundleExecutable`, `CFBundleName`, `CFBundleIdentifier`).
- Linux : `~/.local/share/applications/glane.desktop` avec
  `Exec=sh -c 'exec "$SHELL" -lc "glane app"'`.

L'alternative, des `export` en dur dans le lanceur, est mentionnée pour qui ne
veut pas charger tout son shell.

## Tests

- `go test ./...` reste vert sans écran : rien dans les tests n'ouvre de
  fenêtre.
- Un test vérifie que `web.Handler` sert `/`.
- Vérification manuelle sur macOS avant commit : lancement, recherche, un lien
  s'ouvre dans le navigateur, la fermeture ne laisse aucun processus. Linux
  n'est pas vérifié ; le message de commit le dit.

## Documentation

- `docs/desktop-app.md` : `glane app` et les lanceurs en tête ; la PWA avec un
  service launchd/systemd passe dans une section secondaire « Serveur
  permanent ».
- `docs/commands.md` : une section `glane app`.
- README : une ligne dans le tableau des commandes et le prérequis WebKitGTK
  sous Linux.
- `completions/glane.fish` : la sous-commande `app`.

## Addendum : `glane app --install`

**Problème.** Créer `Glane.app` ou le `.desktop` à la main (heredocs dans la
doc) est fastidieux. Un cask Homebrew livrant un `.app` exige GoReleaser Pro
(`app_bundles`, clé `app:` du cask) et, l'app téléchargée étant mise en
quarantaine, une signature et une notarisation Apple. Un `post_install` de
formule ne peut pas écrire dans `~/Applications`.

**Décision.** Le binaire écrit lui-même le lanceur : `glane app --install`.
Un fichier créé localement n'a pas d'attribut de quarantaine, Gatekeeper ne
s'en mêle pas. La formule le signale dans ses `caveats`. La règle « aucun
flag » ci-dessus devient « un seul flag, `--install` ».

**Vérifié (2026-11-01).** Un `Glane.app` fait main, lancé par le Finder, est
enregistré comme une seule app (`com.glane.app`, bundle `Glane.app`) : la
chaîne `exec` garde le même PID jusqu'au binaire `glane`, donc pas de seconde
icône dans le Dock, et `$SHELL` est défini.

**Comportement.**

- Écrit le lanceur, affiche les chemins créés sur stdout, sort sans ouvrir de
  fenêtre. Relancé, il écrase. Pas de `--uninstall` : la doc dit quoi supprimer.
- Le lanceur appelle `glane` via le PATH du shell de login, jamais un chemin
  absolu : sous Homebrew le vrai chemin est `Cellar/glane/<version>/…` et
  casserait au prochain `brew upgrade`. Si `glane` n'est pas dans le PATH au
  moment de l'install, un avertissement sur stderr.
- macOS, `~/Applications/Glane.app` :
  - `Contents/MacOS/glane` (0755) :
    `exec "$SHELL" -lc 'exec glane app' 2>>"$HOME/Library/Logs/glane-app.log"`
  - `Contents/Info.plist` : `CFBundleExecutable`, `CFBundleName`,
    `CFBundleIdentifier` (`com.glane.app`), `CFBundlePackageType` (`APPL`),
    `CFBundleIconFile` (`glane`), `NSHighResolutionCapable` (sans lui, une app
    en bundle s'affiche floue sur écran Retina).
  - `Contents/Resources/glane.icns` : produit en Go depuis le PNG 512 px
    embarqué (en-tête `icns` + bloc `ic09`), sans `sips`.
- Linux, sous `$XDG_DATA_HOME` (défaut `~/.local/share`) :
  - `applications/glane.desktop` : `Name=Glane`,
    `Exec=sh -c 'exec "$SHELL" -lc "exec glane app"'`, `Icon=glane`,
    `Terminal=false`. GNOME envoie déjà le stderr des apps au journal.
  - `icons/hicolor/512x512/apps/glane.png`.
- Autres OS : erreur `--install supports macOS and Linux`.

**Code.** `installLauncher(goos, home, dataHome string) ([]string, error)` dans
`launcher.go` (sans tag de build : aucune dépendance à glaze), testé sur un
répertoire temporaire.
