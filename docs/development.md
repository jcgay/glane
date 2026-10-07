# Development

## Go binary

- [mise](https://mise.jdx.dev/), which installs the Go version pinned in
  `mise.toml`. Or Go 1.26+ directly, if you prefer not to use mise.

```sh
mise install          # installs the pinned Go
mise run build        # ./glane, self-contained (the web UI assets are embedded)
mise run check        # tests, go vet, gofmt
```

Without mise activated in your shell, prefix Go commands with
`mise exec --`, e.g. `mise exec -- go test ./internal/search/`.

## Android app

The app lives in `android/` and packages the Go binary, cross-compiled
without cgo, so on top of Go it needs:

- **JDK 17 or newer**, the Android Gradle Plugin's minimum (it also builds on
  Temurin 25), e.g. `mise use --global java@temurin-21`.
- **The Android SDK** with Platform 36, Build-Tools 36 and Platform-Tools
  (`adb`). Android Studio's SDK Manager installs them; without Android Studio:

  ```sh
  brew install --cask android-commandlinetools
  sdkmanager --licenses
  sdkmanager "platforms;android-36" "build-tools;36.0.0" "platform-tools"
  ```

- **Where that SDK is**: set `ANDROID_HOME`, or write it to
  `android/local.properties` (ignored by git). Android Studio puts it in
  `~/Library/Android/sdk`, the Homebrew cask in
  `/opt/homebrew/share/android-commandlinetools`:

  ```properties
  sdk.dir=/Users/you/Library/Android/sdk
  ```

Gradle itself, the NDK, gomobile and Android Studio are not needed: the
wrapper downloads Gradle, and Go builds the server alone.

```sh
mise run android      # android/app/build/outputs/apk/debug/app-debug.apk
```

Run it through mise, or with mise activated: the build calls `go`, which
must be on the `PATH` of the Gradle daemon.

To install on a phone, turn on USB debugging (Settings › About phone, tap
*Build number* seven times, then Developer options › USB debugging), plug
it in and accept the prompt:

```sh
adb devices           # the phone, listed as "device"
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

That APK is signed with the debug key in `~/.android/debug.keystore`.
Android installs an update over an app only when both are signed with the
same key, so a debug build and a release build don't mix: uninstall one
before installing the other.

CI builds a debug APK on every pull request and keeps it as a run artifact.
A `v*` tag builds the release APK, signed with the release key held in the
repository secrets, and attaches it to the GitHub release. The release key
is created once:

```sh
keytool -genkeypair -keystore glane-release.jks -alias glane \
  -keyalg RSA -keysize 4096 -validity 10000 -dname "CN=glane"
base64 -i glane-release.jks | gh secret set GLANE_KEYSTORE_BASE64
gh secret set GLANE_KEYSTORE_PASSWORD   # the password keytool asked for
gh secret set GLANE_KEY_PASSWORD        # the same one (keytool's PKCS12 default)
gh secret set GLANE_KEY_ALIAS --body glane
```

Keep `glane-release.jks` and its passwords somewhere safe, out of the
repository: without them no later release can update an installed app, and
everyone would have to uninstall and start over. To build a signed release
locally, point `GLANE_KEYSTORE` at the file and set the three other
variables, then run `./gradlew assembleRelease -PglaneVersion=1.4.0` in
`android/`.

## How it works

- **Storage** — SQLite with an FTS5 mirror kept in sync by triggers, plus a table
  of embedding vectors. Pure-Go driver (`modernc.org/sqlite`), no cgo.
- **Search** — full-text via FTS5 `bm25`; semantic via brute-force cosine over
  stored vectors; the two are fused with Reciprocal Rank Fusion when both are
  available. The CLI and the web UI share one `search.Hybrid` entry point.
- **Enrichment** — article extraction via `go-shiori/go-readability`.
- **Summaries & tags** — one optional chat call per article yields a summary and
  free tags; the summary is additive (never replaces the indexed article text)
  and embeddings are left as-is.
- **Progress** — `sync`, `enrich`, and `summarize` print live progress to
  **stderr** while they run; the final summary goes to **stdout**, so piping
  (`glane search … | …`) stays clean.
