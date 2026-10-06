# glane — application Android hors ligne

**Date :** 2026-10-06
**Statut :** design validé

## Problème

La veille n'est consultable que depuis le Mac : `glane serve` écoute sur
`127.0.0.1`. Impossible de retrouver un article depuis le téléphone, a fortiori
sans réseau (métro, avion).

## Objectif

Une application Android qui permet de chercher dans sa veille **hors ligne**, en
**lecture seule**, à partir de la base `glane.db` synchronisée depuis le Mac.

Critère de réussite : dans le métro, sans réseau, ouvrir l'app, taper
« cold start » et retrouver l'article en quelques secondes.

## Hypothèses

- Le téléphone est sous Android (arm64, Android 11 ou plus récent).
- `glane.db` arrive sur le téléphone par **Syncthing-Fork**
  (`researchxxl/syncthing-android`, via F-Droid), depuis le dossier
  `~/Sync/glane` déjà utilisé sur le Mac. Le dossier est déclaré
  **« Recevoir uniquement »** côté téléphone.
- Le Mac reste le seul à écrire dans la base. La base fait ~180 Mo aujourd'hui.

## Hors périmètre

Recherche sémantique hors ligne, écriture depuis le téléphone (tags, lu/non lu,
ajout de liens), iOS, Play Store, mise à jour automatique de l'app, CI Android.

## Architecture

Trois éléments, tous dans ce repo.

### 1. `glane serve --read-only` (Go)

Le binaire Go reste le cœur : même serveur, même UI, même `search.Hybrid`.

- **Ouverture en lecture seule** : `store.Open` exécute aujourd'hui le schéma
  (`CREATE TABLE IF NOT EXISTS…`), donc écrit. Une ouverture lecture seule
  (`mode=ro&immutable=1`) n'exécute pas le schéma, ne crée ni `-wal` ni `-shm`
  et ne prend pas de verrou. Une base absente est une erreur (elle n'est jamais
  créée).
- **Rechargement quand la base change** : Syncthing écrit dans un fichier
  temporaire puis le renomme. À chaque requête, le serveur fait un `stat` du
  chemin ; si l'inode ou le mtime a changé, il ouvre la nouvelle base et ferme
  l'ancienne. Même après le renommage, l'ancien descripteur lit toujours
  l'ancienne version intacte : jamais d'état à moitié écrit. `immutable=1` est sûr pour
  la même raison, puisqu'un fichier donné n'est jamais modifié en place.
- **Activation** : flag `--read-only` sur `serve`. Sans lui, le comportement
  actuel est inchangé.
- **Sémantique** : aucun `GLANE_EMBED_URL` n'est passé au processus sur le
  téléphone, donc la recherche est FTS seule, comme `search.Hybrid` le prévoit
  déjà. La barre d'état l'affiche.

### 2. L'app Android (Kotlin)

Coquille minimale autour du binaire Go, sur le modèle de Syncthing-Android
lui-même : pas de `gomobile`, le binaire est lancé en sous-processus.

- Le binaire est compilé avec `GOOS=android GOARCH=arm64 CGO_ENABLED=0` et
  embarqué sous `jniLibs/arm64-v8a/libglane.so`. Android installe les `jniLibs`
  dans un dossier exécutable (`extractNativeLibs=true`).
- Une seule Activity avec une WebView. Au démarrage : trouver un port libre,
  lancer `libglane.so serve --read-only --port N` avec
  `GLANE_DB=<chemin réglé>`, attendre que le port réponde, charger
  `http://127.0.0.1:N/`.
- Le bouton retour fait `history.back()` dans la WebView. Les liens externes
  (articles, posts) s'ouvrent dans le navigateur par défaut.
- Le sous-processus est arrêté quand l'Activity est détruite.
- Un écran de réglages avec un seul champ : le chemin de `glane.db`, par défaut
  `/storage/emulated/0/Sync/glane/glane.db`, mémorisé dans les
  SharedPreferences.
- **Accès au fichier** : permission « Accès à tous les fichiers »
  (`MANAGE_EXTERNAL_STORAGE`), demandée au premier lancement. Acceptable pour
  une app personnelle installée hors Play Store. L'alternative SAF (sélecteur de
  document + copie de 180 Mo dans l'app à chaque changement) a été écartée :
  plus lourde et plus lente.
- Aucune bibliothèque tierce : Kotlin, WebView et SharedPreferences du
  framework. `minSdk` 30, `targetSdk` à la dernière version.

### 3. UI web adaptée au tactile

La même UI que sur le Mac, avec des media queries pour les écrans étroits :
rail de filtres repliable, cibles tactiles plus grandes, raccourcis clavier en
retrait, pas de défilement horizontal. Le Mac n'est pas affecté au-delà de la
largeur de rupture.

## Comportement et cas d'erreur

| Situation | Comportement |
|---|---|
| Premier lancement | Demande de la permission, puis proposition du chemin par défaut (modifiable). |
| Permission refusée | Écran natif expliquant pourquoi elle est nécessaire, bouton vers les réglages système. |
| Base absente ou illisible | Écran natif « glane.db introuvable à <chemin> » avec un bouton Réglages. Le serveur n'est pas lancé. |
| Serveur qui ne démarre pas ou plante | Dernière ligne de stderr affichée, bouton « Relancer ». Pas de redémarrage automatique en boucle. |
| Retour au premier plan après une pause | Si le port ne répond plus (processus tué par Android), relance puis rechargement de l'URL courante : la requête `?q=…` est conservée. |
| Base mise à jour pendant l'utilisation | Rechargement transparent côté Go (`stat` à chaque requête). |
| Pas de réseau | La recherche fonctionne ; seuls les liens externes en ont besoin. |

## Build et installation

- Projet Gradle dans `android/`, un module `app`. Une tâche Gradle compile le
  binaire Go dans `jniLibs` avant l'assemblage de l'APK.
- Seule cible : `arm64-v8a`.
- Commande unique : `./gradlew assembleRelease` dans `android/`, aussi exposée
  en tâche mise `android`.
- APK signé avec une clé locale. Le keystore reste hors du repo, son chemin et
  ses mots de passe sont passés par variables d'environnement.
- Installation par `adb install` ou en transférant l'APK sur le téléphone.

## Tests

- **Go, en TDD** :
  - ouverture lecture seule : une base existante est lisible et reste identique
    octet pour octet après des recherches ; une base absente renvoie une erreur
    au lieu d'être créée ;
  - rechargement : remplacer le fichier par renommage pendant que le handler
    tourne, la requête suivante voit les nouvelles données ;
  - `serve --read-only` couvert dans les tests existants de `main` / `web`.
- **UI tactile** : vérification playwright à la largeur d'un téléphone (rail
  replié, résultats lisibles, pas de défilement horizontal), lancée à la main.
- **App Android** : pas de tests instrumentés, la couche Kotlin est trop mince
  pour les justifier. Vérification manuelle sur l'émulateur du SDK avec une
  copie de la base : premier lancement, base absente, processus tué, base
  remplacée pendant l'utilisation.

## Documentation

- Section « Android » dans le README : installer Syncthing-Fork depuis F-Droid,
  partager `~/Sync/glane` en « Recevoir uniquement », installer l'APK, accorder
  la permission, régler le chemin.
- Flag `--read-only` documenté dans le README et ajouté à
  `completions/glane.fish`.
