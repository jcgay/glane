# glane Android app Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Search the glane index offline on an Android phone, read-only, from the `glane.db` that Syncthing-Fork brings over from the Mac.

**Architecture:** The Go binary stays the core. A new `glane serve --read-only` opens the database with `mode=ro&immutable=1` and reopens it whenever the file is replaced. A minimal Kotlin app ships that binary as `libglane.so`, runs it as a subprocess on a free localhost port and shows the existing web UI in a WebView.

**Tech Stack:** Go (pure Go, `modernc.org/sqlite`), Kotlin through AGP 9 built-in Kotlin, Android framework only (no AndroidX), Gradle wrapper 9.8.0.

**Spec:** `docs/superpowers/specs/2026-10-06-android-app-design.md`

**Deviations from the spec, decided while planning:**
- **Phone layout:** the UI already has a `@media (max-width: 760px)` layout (`internal/web/templates/index.html:209-224`). Task 4 checks it at phone width and fixes only what fails, instead of designing a new one.
- **Manual testing:** the SDK on this Mac has no emulator system image. Manual checks run on the real phone over `adb` (USB debugging).
- **Signing:** the APK is debug-signed (`assembleDebug`, key in `~/.android/debug.keystore`), not signed with a dedicated release keystore. For a personal sideloaded app this is enough, and it stays stable as long as builds come from this Mac.

## Global Constraints

- Go stays pure Go: `CGO_ENABLED=0`, no new Go dependency. `go vet ./...` stays clean and `gofmt -l .` stays empty (`mise run check`).
- `glane serve` without `--read-only` behaves exactly as today.
- `serve --read-only` never writes the database file: no schema, no `-wal`/`-shm`/`-journal` file, no newly created file.
- Android: `minSdk = 30`, `targetSdk = 36`, `compileSdk = 36`, ABI `arm64-v8a` only, application id `fr.jcgay.glane`.
- No third-party Android library, AndroidX included: framework `Activity`, `WebView` and `SharedPreferences` only.
- AGP `9.0.1`, or the newest stable 9.x if a later one exists (check https://developer.android.com/build/releases/gradle-plugin). Built-in Kotlin, no `org.jetbrains.kotlin.android` plugin.
- Default database path on the phone: `/storage/emulated/0/Sync/glane/glane.db`.
- Strings shown by the Android shell are in French. The web UI keeps its existing i18n.
- Gradle runs through `build-brief` (`build-brief ./gradlew …`).
- Every commit follows the repo style (gitmoji subject, English, imperative) and ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Wrong file picked in the settings** (a non-SQLite file, or a SQLite file without glane tables): `serve --read-only` must exit at startup with a readable error so the app can show it, not serve a blank page. → Task 1 test `TestOpenReadOnlyRejectsForeignFile`.
2. **A truncated or corrupt copy replaces the file** (sync interrupted, bad transfer): keep serving the last good copy. → Task 2 test `TestReloaderKeepsLastGoodCopy`.
3. **The file disappears for a moment** (deleted before the new copy is renamed in): keep serving the last good copy, then pick up the new one. → Task 2 test `TestReloaderSurvivesMissingFile`.
4. **Android kills the Go subprocess in the background** (phantom process killer, Android 12+): coming back to the app must restart it and restore the same query. → Task 6 manual check, step "process killed".
5. **Tapping a result link**: the article opens in the phone's browser, while htmx requests to `127.0.0.1` stay in the WebView. → Task 6 manual check, step "links".

---

### Task 1: Read-only store

**Files:**
- Modify: `internal/store/store.go` (imports, new `OpenReadOnly` after `Open`, ~line 111)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `func OpenReadOnly(path string) (*Store, error)`. It returns an error if the file is missing, is not SQLite, or has no `items` table. It never creates or writes the file.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/store_test.go` and add `"os"` to its imports:

```go
func TestOpenReadOnlyNeverWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "glane.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start on lambda", URL: "https://x/1"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	before, _ := os.ReadFile(p)

	ro, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ro.SearchFTS("cold", Filter{})
	if err != nil || len(res) != 1 {
		t.Fatalf("SearchFTS = %d results, %v; want 1", len(res), err)
	}
	if _, err := ro.Upsert([]Item{{Source: "twitter", SourceID: "2", Text: "nope"}}); err == nil {
		t.Error("Upsert on a read-only store succeeded")
	}
	ro.Close()

	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Error("database file changed after read-only use")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("read-only use left extra files: %v", entries)
	}
}

func TestOpenReadOnlyMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.db")
	if _, err := OpenReadOnly(p); err == nil {
		t.Fatal("OpenReadOnly on a missing file succeeded")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("OpenReadOnly created the missing file")
	}
}

func TestOpenReadOnlyRejectsForeignFile(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.txt")
	os.WriteFile(text, []byte("not a database"), 0o644)
	if _, err := OpenReadOnly(text); err == nil {
		t.Error("OpenReadOnly accepted a text file")
	}

	other := filepath.Join(dir, "other.db")
	db, err := sql.Open("sqlite", other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE contacts(name TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := OpenReadOnly(other); err == nil {
		t.Error("OpenReadOnly accepted a SQLite file without glane tables")
	}
}
```

Also add `"database/sql"` to the test imports (`modernc.org/sqlite` is already registered by `store.go`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `mise exec -- go test ./internal/store/ -run OpenReadOnly`
Expected: build failure, `undefined: OpenReadOnly`.

- [ ] **Step 3: Implement `OpenReadOnly`**

In `internal/store/store.go`, add `"fmt"` to the imports and this function right after `Open`:

```go
// OpenReadOnly opens an existing database without ever writing to it: no
// schema, no journal file, no lock. immutable=1 is safe because a synced copy
// is only ever replaced whole (the sync tool renames a finished download over
// it), never changed in place; to see a new copy, open it again.
func OpenReadOnly(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	// sql.Open is lazy: touch items so that a missing file or a database that
	// isn't glane's fails here rather than on the first search
	if _, err := db.Exec("SELECT 1 FROM items LIMIT 1"); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s read-only: %w", path, err)
	}
	return &Store{db: db}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `mise exec -- go test ./internal/store/`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit
```

Message:

```
✨ Open the database read-only

A phone that only receives glane.db from Syncthing must never write it:
a write there would come back to the Mac as a sync conflict. Open runs
the schema, so a separate opener skips it and uses SQLite's immutable
mode, which also leaves no journal file next to the database.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

### Task 2: Serve a database that gets replaced

**Files:**
- Create: `internal/web/readonly.go`
- Test: `internal/web/readonly_test.go`

**Interfaces:**
- Consumes: `store.OpenReadOnly(path string) (*store.Store, error)` (Task 1). Also the existing unexported `handler(s *store.Store) http.Handler` in `internal/web/web.go`.
- Produces: `func ServeReadOnly(path, addr string) error`. It opens the file before listening and returns the open error right away if that fails.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/readonly_test.go`:

```go
package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

// writeDB builds a glane database holding one item at path, through a temp
// file renamed over it: the same way Syncthing delivers a new copy.
func writeDB(t *testing.T, path, srcid, text string) {
	t.Helper()
	tmp := path + ".tmp"
	os.Remove(tmp)
	s, err := store.Open(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]store.Item{{Source: "bluesky", SourceID: srcid, Kind: "like", Text: text, URL: "http://x/" + srcid}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func search(t *testing.T, r *reloader, q string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/search?q="+q, nil))
	if rec.Code != 200 {
		t.Fatalf("GET /search?q=%s: status %d: %s", q, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func TestReloaderPicksUpReplacedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glane.db")
	writeDB(t, p, "1", "alpha")
	r := &reloader{path: p}

	if !strings.Contains(search(t, r, "alpha"), "http://x/1") {
		t.Fatal("first copy not served")
	}
	writeDB(t, p, "2", "beta")
	if !strings.Contains(search(t, r, "beta"), "http://x/2") {
		t.Fatal("replaced copy not picked up")
	}
}

func TestReloaderKeepsLastGoodCopy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glane.db")
	writeDB(t, p, "1", "alpha")
	r := &reloader{path: p}
	search(t, r, "alpha")

	tmp := p + ".tmp"
	os.WriteFile(tmp, []byte("truncated download"), 0o644)
	os.Rename(tmp, p)
	if !strings.Contains(search(t, r, "alpha"), "http://x/1") {
		t.Fatal("a corrupt copy replaced the last good one")
	}
}

func TestReloaderSurvivesMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glane.db")
	writeDB(t, p, "1", "alpha")
	r := &reloader{path: p}
	search(t, r, "alpha")

	os.Remove(p)
	if !strings.Contains(search(t, r, "alpha"), "http://x/1") {
		t.Fatal("a missing file dropped the last good copy")
	}
	writeDB(t, p, "2", "beta")
	if !strings.Contains(search(t, r, "beta"), "http://x/2") {
		t.Fatal("the copy arriving after the gap was not picked up")
	}
}

func TestServeReadOnlyFailsFastOnMissingFile(t *testing.T) {
	err := ServeReadOnly(filepath.Join(t.TempDir(), "missing.db"), "127.0.0.1:0")
	if err == nil {
		t.Fatal("ServeReadOnly started without a database")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `mise exec -- go test ./internal/web/ -run 'Reloader|ServeReadOnly'`
Expected: build failure, `undefined: reloader` and `undefined: ServeReadOnly`.

- [ ] **Step 3: Implement the reloader**

Create `internal/web/readonly.go`:

```go
package web

import (
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jcgay/glane/internal/store"
)

// ServeReadOnly serves the database at path without ever writing to it, and
// picks up a new copy as soon as one replaces the file (a sync tool renaming
// its download over it), without a restart.
func ServeReadOnly(path, addr string) error {
	r := &reloader{path: path}
	if _, err := r.current(); err != nil {
		return err
	}
	return http.ListenAndServe(addr, r)
}

// reloader is a handler over the newest readable copy of one database file.
type reloader struct {
	path string

	mu sync.Mutex
	fi os.FileInfo // the copy last tried, good or not
	s  *store.Store
	h  http.Handler
}

func (r *reloader) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h, err := r.current()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, req)
}

// current reopens the file when a different copy sits at path. A missing file
// or a copy that won't open keeps the last good one in service.
func (r *reloader) current() (http.Handler, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fi, err := os.Stat(r.path)
	if err == nil && r.fi != nil && os.SameFile(fi, r.fi) && fi.ModTime().Equal(r.fi.ModTime()) {
		return r.h, nil
	}
	var s *store.Store
	if err == nil {
		s, err = store.OpenReadOnly(r.path)
	}
	if err != nil {
		if r.h == nil {
			return nil, err // first open: ServeReadOnly reports it and exits
		}
		if fi != nil {
			r.fi = fi // try a bad copy once, not on every request
		}
		log.Printf("glane: keeping the previous database: %v", err)
		return r.h, nil
	}
	if old := r.s; old != nil {
		// ponytail: requests still running on the old copy get a minute to
		// finish before it closes; a slower one would fail mid-query
		time.AfterFunc(time.Minute, func() { old.Close() })
	}
	r.fi, r.s, r.h = fi, s, handler(s)
	return r.h, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `mise exec -- go test ./internal/web/`
Expected: PASS (whole package, existing tests included).

- [ ] **Step 5: Run the race detector on the new tests**

Run: `mise exec -- go test -race ./internal/web/ -run 'Reloader|ServeReadOnly'`
Expected: PASS, no `DATA RACE` report.

- [ ] **Step 6: Commit**

```bash
git add internal/web/readonly.go internal/web/readonly_test.go
git commit
```

Message:

```
✨ Reopen a read-only database when its file is replaced

Syncthing delivers a new glane.db by renaming a finished download over
the old one, so a long-running server would keep searching the copy it
opened first. Each request now checks whether another file sits at the
path and opens it. A copy that fails to open (an interrupted transfer)
or a file missing for a moment leaves the last good copy in service.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

### Task 3: `glane serve --read-only`

**Files:**
- Modify: `main.go` (`main` around lines 49-70, `cmdServe` around line 359)
- Modify: `README.md` (section `### glane serve [--port N]`, line 255)
- Modify: `completions/glane.fish` (after line 29)

**Interfaces:**
- Consumes: `web.ServeReadOnly(path, addr string) error` (Task 2), `dbPath() string`, `fatal(error)`.
- Produces: the CLI contract the Android app relies on: `glane serve --read-only --port N` with `GLANE_DB=<path>`. It prints `glane serving on http://127.0.0.1:N` on stdout. If the database can't be opened, it exits with status 1 and `glane: <error>` as the last stderr line.

- [ ] **Step 1: Route `serve` before the read-write open**

`main` opens the database read-write (and creates it) before dispatching, which `--read-only` must avoid. In `main.go`, insert right before `s, err := store.Open(dbPath())`:

```go
	// serve opens the database itself: --read-only must never create or write it
	if os.Args[1] == "serve" {
		cmdServe(os.Args[2:])
		return
	}
```

and delete these two lines from the `switch`:

```go
	case "serve":
		cmdServe(s, os.Args[2:])
```

- [ ] **Step 2: Add the flag to `cmdServe`**

Replace the whole `cmdServe` function with:

```go
func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "listen port")
	readOnly := fs.Bool("read-only", false, "never write the database; reopen it when a sync replaces the file")
	fs.Parse(args)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	if *readOnly {
		fmt.Printf("glane serving %s read-only on http://%s\n", dbPath(), addr)
		if err := web.ServeReadOnly(dbPath(), addr); err != nil {
			fatal(err)
		}
		return
	}
	s, err := store.Open(dbPath())
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	fmt.Printf("glane serving on http://%s\n", addr)
	if err := web.Serve(s, addr); err != nil {
		fatal(err)
	}
}
```

- [ ] **Step 3: Build, vet and test**

Run: `mise run check && mise run build`
Expected: tests pass, vet clean, gofmt clean, `./glane` built.

- [ ] **Step 4: Check the CLI by hand**

```sh
d=$(mktemp -d)
GLANE_DB=$d/missing.db ./glane serve --read-only --port 18080; echo "exit=$?"; ls $d
```
Expected: `glane: open …/missing.db read-only: …` on stderr, `exit=1`, and `ls` prints nothing (no file created).

```sh
cp ~/Sync/glane/glane.db $d/glane.db && shasum $d/glane.db
GLANE_DB=$d/glane.db ./glane serve --read-only --port 18080 &
sleep 1; curl -s 'http://127.0.0.1:18080/search?q=lambda' | grep -c 'class="hit'
kill %1; shasum $d/glane.db; ls $d
```
Expected: a non-zero hit count, the same checksum twice, and only `glane.db` in `$d`.

- [ ] **Step 5: Document the flag**

In `README.md`, change the heading `### \`glane serve [--port N]\`` to `### \`glane serve [--port N] [--read-only]\``, and add this bullet at the end of that section's list (after the **Index health** bullet):

```markdown
- **`--read-only`** never writes the database: no schema migration, no
  journal file, and a missing file is an error rather than a new empty
  database. When a sync tool replaces the file, the next request picks up
  the new copy without a restart. This is how the Android app serves the
  copy Syncthing brings to the phone.
```

In `completions/glane.fish`, after the `-l port` line for `serve`, add:

```fish
complete -c glane -n "__fish_seen_subcommand_from serve"     -l read-only -d "never write the database; reload it when replaced"
```

- [ ] **Step 6: Commit**

```bash
git add main.go README.md completions/glane.fish
git commit
```

Message:

```
✨ Add serve --read-only

The Android app runs glane serve over the copy of glane.db that
Syncthing keeps on the phone, which must never be written. main opened
the database read-write, creating it if missing, before dispatching
any command, so serve now opens it itself and can pick the read-only
reloader instead.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

### Task 4: Phone-width check of the web UI

**Files:**
- Modify (only if a check fails): `internal/web/templates/index.html`, inside the `@media (max-width: 760px)` block (lines 209-224)

**Interfaces:**
- Consumes: `./glane serve --read-only` (Task 3).
- Produces: a UI usable at 390×844 CSS px, the viewport the WebView gets on a typical phone.

- [ ] **Step 1: Serve the real database read-only**

```sh
mise run build
GLANE_DB=~/Sync/glane/glane.db ./glane serve --read-only --port 18080 &
```

- [ ] **Step 2: Inspect at phone width with playwright**

Load the `playwright-cli` skill. Open `http://127.0.0.1:18080/?q=lambda` at viewport 390×844, then:
- take a screenshot (light and dark color scheme);
- evaluate `document.documentElement.scrollWidth <= window.innerWidth`, which must be `true` (no horizontal scroll);
- evaluate `getComputedStyle(document.querySelector('.rail')).display` and check the source/period filters are visible and tappable;
- tap a source filter and check the result list updates;
- check that the search input, each result title and the filter buttons are at least 44 px tall or have enough spacing to tap (WCAG 2.5.8 target size, minimum 24 px).

- [ ] **Step 3: Fix only what failed**

Each fix goes in the `@media (max-width: 760px)` block, so the desktop layout can't change. For example, if the facet buttons are under 24 px tall:

```css
      .facet { min-height: 32px; }
```

If everything passes, record "phone layout OK, no change" in the task report and skip Step 4.

- [ ] **Step 4: Re-run Step 2, then commit**

```bash
mise run check
git add internal/web/templates/index.html
git commit
```

Message (adapt the subject to the actual fix):

```
💄 Enlarge filter tap targets on phones

<what failed at 390 px wide and why it matters on a touch screen>

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

- [ ] **Step 5: Stop the server**

`kill %1`

---

### Task 5: Android build that packages the Go binary

**Files:**
- Create: `android/settings.gradle.kts`
- Create: `android/build.gradle.kts`
- Create: `android/gradle.properties`
- Create: `android/app/build.gradle.kts`
- Create: `android/app/src/main/AndroidManifest.xml` (placeholder, replaced in Task 6)
- Create: `android/gradlew`, `android/gradlew.bat`, `android/gradle/wrapper/*` (generated)
- Create: `android/local.properties` (not committed)
- Modify: `.gitignore`, `mise.toml`

**Interfaces:**
- Consumes: the Go module at the repo root, built with `GOOS=android GOARCH=arm64 CGO_ENABLED=0`.
- Produces: `android/app/build/outputs/apk/debug/app-debug.apk`, which contains `lib/arm64-v8a/libglane.so`. Gradle task `:app:goBinary`, mise task `android`.

- [ ] **Step 1: Gradle settings and root build**

`android/settings.gradle.kts`:

```kotlin
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "glane-android"
include(":app")
```

`android/build.gradle.kts`:

```kotlin
plugins {
    id("com.android.application") version "9.0.1" apply false
}
```

`android/gradle.properties`:

```properties
org.gradle.jvmargs=-Xmx2g
org.gradle.configuration-cache=true
```

`android/local.properties` (machine-specific, ignored by git):

```properties
sdk.dir=/Users/jcgay/Library/Android/sdk
```

- [ ] **Step 2: App module with the Go build**

`android/app/build.gradle.kts`:

```kotlin
plugins {
    id("com.android.application")
}

android {
    namespace = "fr.jcgay.glane"
    compileSdk = 36
    defaultConfig {
        applicationId = "fr.jcgay.glane"
        minSdk = 30
        targetSdk = 36
        versionCode = 1
        versionName = "0.1"
        ndk { abiFilters += "arm64-v8a" }
    }
    // the Go server is launched as a process: it must sit on disk, in the
    // app's native library dir, the one place Android lets an app execute
    packaging { jniLibs { useLegacyPackaging = true } }
}

val repoRoot = rootProject.layout.projectDirectory.dir("..")
val goBinary = tasks.register<Exec>("goBinary") {
    description = "Cross-compiles glane for Android as libglane.so"
    val out = layout.projectDirectory.file("src/main/jniLibs/arm64-v8a/libglane.so")
    inputs.files(fileTree(repoRoot) {
        include("**/*.go", "go.mod", "go.sum", "internal/web/templates/**", "internal/web/static/**")
        exclude("android/**")
    })
    outputs.file(out)
    workingDir(repoRoot)
    environment("GOOS", "android")
    environment("GOARCH", "arm64")
    environment("CGO_ENABLED", "0")
    commandLine("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out.asFile.path, ".")
}
tasks.named("preBuild") { dependsOn(goBinary) }
```

`android/app/src/main/AndroidManifest.xml` (placeholder until Task 6):

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">
    <application android:label="glane" />
</manifest>
```

- [ ] **Step 3: Ignore build output**

Append to `.gitignore`:

```gitignore
# android
/android/.gradle/
/android/.kotlin/
/android/build/
/android/app/build/
/android/local.properties
/android/app/src/main/jniLibs/
```

- [ ] **Step 4: Generate the wrapper**

Run: `cd android && build-brief gradle wrapper --gradle-version 9.8.0`
Expected: `android/gradlew` and `android/gradle/wrapper/gradle-wrapper.properties` created.

- [ ] **Step 5: Add the mise task**

Append to `mise.toml`:

```toml
[tasks.android]
description = "Build the Android app (debug-signed APK, Go server included)"
dir = "android"
run = "./gradlew assembleDebug"
```

- [ ] **Step 6: Build and check the APK**

Run: `cd android && build-brief ./gradlew assembleDebug`
Expected: BUILD SUCCESSFUL. If Kotlin or AGP rejects the JDK (temurin-25), run with JDK 21 instead: `mise use --path android java@temurin-21`, which writes `android/mise.toml`. Commit that file with this task.

Run: `unzip -l android/app/build/outputs/apk/debug/app-debug.apk | grep libglane.so`
Expected: one line, `lib/arm64-v8a/libglane.so`.

Run: `cd android && build-brief ./gradlew assembleDebug` again.
Expected: `:app:goBinary` reported UP-TO-DATE (no Go source changed).

- [ ] **Step 7: Commit**

```bash
git add .gitignore mise.toml android/settings.gradle.kts android/build.gradle.kts android/gradle.properties android/app/build.gradle.kts android/app/src/main/AndroidManifest.xml android/gradlew android/gradlew.bat android/gradle
git commit
```

Message:

```
🏗️ Package the Go server in an Android build

The phone app is a shell around glane serve, so the APK has to carry
the Go binary. It is cross-compiled without cgo and shipped as
libglane.so: Android installs native libraries in the only directory an
app may execute from, the trick Syncthing-Android uses for its own
binary. That also avoids gomobile and the NDK.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

---

### Task 6: The Android shell

**Files:**
- Create: `android/app/src/main/kotlin/fr/jcgay/glane/MainActivity.kt`
- Modify: `android/app/src/main/AndroidManifest.xml` (replace the placeholder)
- Modify: `README.md` (new `## Android` section between `## Sharing across machines` and `## Environment variables`)

**Interfaces:**
- Consumes: `libglane.so serve --read-only --port N` with `GLANE_DB`, and the startup/exit contract from Task 3. The APK build comes from Task 5.
- Produces: the installable app.

- [ ] **Step 1: Manifest**

Replace `android/app/src/main/AndroidManifest.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

    <!-- read glane.db where Syncthing drops it, in shared storage -->
    <uses-permission android:name="android.permission.MANAGE_EXTERNAL_STORAGE" />

    <!-- cleartext is only ever http://127.0.0.1: external links go to the browser -->
    <application
        android:label="glane"
        android:theme="@android:style/Theme.DeviceDefault.DayNight"
        android:usesCleartextTraffic="true"
        android:enableOnBackInvokedCallback="true">
        <!-- configChanges: rotating must not recreate the activity and kill the server -->
        <activity
            android:name=".MainActivity"
            android:exported="true"
            android:configChanges="orientation|screenSize|screenLayout|smallestScreenSize|keyboardHidden|uiMode">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
```

- [ ] **Step 2: Activity**

Create `android/app/src/main/kotlin/fr/jcgay/glane/MainActivity.kt`:

```kotlin
package fr.jcgay.glane

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.Settings
import android.view.WindowInsets
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import android.window.OnBackInvokedDispatcher
import java.io.File
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket

private const val DEFAULT_DB = "/storage/emulated/0/Sync/glane/glane.db"

/**
 * Runs `glane serve --read-only` from the APK's native library dir and shows
 * it in a WebView. Everything glane does happens in that process; this class
 * only starts it, restarts it when Android killed it, and explains failures.
 */
class MainActivity : Activity() {
    private lateinit var root: FrameLayout
    private lateinit var web: WebView
    private var server: Process? = null
    private var port = 0
    @Volatile private var lastLine = ""
    private val prefs by lazy { getSharedPreferences("glane", MODE_PRIVATE) }

    private var dbPath: String
        get() = prefs.getString("db", DEFAULT_DB)!!
        set(value) = prefs.edit().putString("db", value).apply()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        root = FrameLayout(this)
        // targetSdk 35+ draws edge to edge: keep content clear of the bars and the keyboard
        root.setOnApplyWindowInsetsListener { v, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.ime())
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            WindowInsets.CONSUMED
        }
        setContentView(root)
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                    if (request.url.host == "127.0.0.1") return false
                    startActivity(Intent(Intent.ACTION_VIEW, request.url))
                    return true
                }
            }
        }
        if (Build.VERSION.SDK_INT >= 33) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(OnBackInvokedDispatcher.PRIORITY_DEFAULT) { back() }
        }
    }

    @Deprecated("Android 12 and older only; 13+ goes through onBackInvokedDispatcher")
    override fun onBackPressed() = back()

    private fun back() {
        if (root.getChildAt(0) === web && web.canGoBack()) web.goBack() else finish()
    }

    override fun onResume() {
        super.onResume()
        start()
    }

    override fun onDestroy() {
        server?.destroy()
        web.destroy()
        super.onDestroy()
    }

    private fun start() {
        if (!Environment.isExternalStorageManager()) {
            return show(
                "glane lit glane.db dans le dossier synchronisé par Syncthing : il a besoin de l'accès à tous les fichiers.",
                "Autoriser" to {
                    startActivity(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:$packageName")))
                },
            )
        }
        if (!File(dbPath).canRead()) {
            return show("glane.db introuvable à $dbPath", "Réglages" to ::askPath, "Réessayer" to ::start)
        }
        // still running: nothing to do. Dead (Android kills background child
        // processes): restart it and come back to the same page.
        if (server?.isAlive == true) return
        val resume = web.url?.substringAfter("127.0.0.1:$port", "/") ?: "/"
        port = ServerSocket(0).use { it.localPort }
        lastLine = ""
        val p = ProcessBuilder(File(applicationInfo.nativeLibraryDir, "libglane.so").path, "serve", "--read-only", "--port", "$port")
            .redirectErrorStream(true)
            .apply { environment()["GLANE_DB"] = dbPath }
            .start()
        server = p
        Thread { p.inputStream.bufferedReader().forEachLine { lastLine = it } }.start()
        show("Démarrage de glane…")
        Thread {
            val deadline = System.currentTimeMillis() + 10_000
            while (p.isAlive && !listening(port) && System.currentTimeMillis() < deadline) Thread.sleep(50)
            val up = p.isAlive && listening(port)
            runOnUiThread {
                if (server !== p) return@runOnUiThread
                if (up) {
                    showWeb()
                    web.loadUrl("http://127.0.0.1:$port$resume")
                } else {
                    p.destroy()
                    show("glane n'a pas démarré : $lastLine", "Relancer" to ::start, "Réglages" to ::askPath)
                }
            }
        }.start()
    }

    private fun listening(port: Int) =
        runCatching { Socket().use { it.connect(InetSocketAddress("127.0.0.1", port), 200) } }.isSuccess

    private fun askPath() {
        val input = EditText(this).apply {
            setText(dbPath)
            setSingleLine()
        }
        AlertDialog.Builder(this)
            .setTitle("Chemin de glane.db")
            .setView(input)
            .setPositiveButton("OK") { _, _ ->
                dbPath = input.text.toString().trim()
                server?.destroy()
                server = null
                start()
            }
            .setNegativeButton("Annuler", null)
            .show()
    }

    private fun showWeb() {
        if (root.getChildAt(0) === web) return
        root.removeAllViews()
        root.addView(web)
    }

    private fun show(text: String, vararg actions: Pair<String, () -> Unit>) {
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 48, 48, 48)
            addView(TextView(context).apply {
                this.text = text
                textSize = 16f
            })
            actions.forEach { (label, action) ->
                addView(Button(context).apply {
                    this.text = label
                    setOnClickListener { action() }
                })
            }
        }
        root.removeAllViews()
        root.addView(box)
    }
}
```

- [ ] **Step 3: Build**

Run: `mise run android`
Expected: BUILD SUCCESSFUL.

- [ ] **Step 4: Install on the phone**

On the phone: Settings › About › tap "Build number" 7 times, then Developer options › USB debugging on. Plug it in and accept the prompt.

Run: `~/Library/Android/sdk/platform-tools/adb devices`
Expected: one device listed as `device`.

Run: `~/Library/Android/sdk/platform-tools/adb install -r android/app/build/outputs/apk/debug/app-debug.apk`
Expected: `Success`.

- [ ] **Step 5: Manual checks on the phone**

Prerequisite: Syncthing-Fork (F-Droid) installed, folder `~/Sync/glane` shared to the phone as **Receive Only**, synced into `/storage/emulated/0/Sync/glane/`. Follow logs with `adb logcat | grep -i glane` if something goes wrong.

Check each, and write the result in the task report:
1. **First launch**: the permission screen appears. "Autoriser" opens the system page; grant it and go back. The web UI loads.
2. **Search**: type `lambda`, results appear. Airplane mode on: search still works.
3. **Links** (Review Focus 5): tap a result title, the browser opens the article. Back returns to glane on the same results. Tapping a source filter stays in the app.
4. **Back**: after two searches, Back goes through history, then leaves the app.
5. **Process killed** (Review Focus 4): with a query on screen, run `adb shell pkill -f libglane.so` (or `adb shell am kill fr.jcgay.glane` with the app in background), then reopen the app. The server restarts and the same query is shown.
6. **Database missing**: Réglages › enter `/storage/emulated/0/nope.db`. The "introuvable" screen shows the path. Set the right path back and the UI loads.
7. **Wrong file**: point the path to any non-database file the app can read, e.g. a photo in `/storage/emulated/0/DCIM/`. The "n'a pas démarré" screen shows `glane: open … read-only: …`.
8. **Sync while open**: on the Mac, run `glane sync all`. Once Syncthing has delivered the new file, a new search on the phone finds the new items without restarting the app.
9. **Rotation and dark mode**: rotate the phone, the page stays and no "Démarrage" screen appears. Toggle the system dark theme, the UI follows.
10. **Keyboard**: the search box stays visible above the keyboard, and the status bar doesn't overlap the page.

Fix any failure in `MainActivity.kt` or the manifest, rebuild, reinstall, re-check.

- [ ] **Step 6: Document the app**

In `README.md`, add between `## Sharing across machines` and `## Environment variables`:

````markdown
## Android

An Android app searches your index offline, read-only, over the copy of
`glane.db` that Syncthing keeps on the phone. It wraps `glane serve
--read-only` and shows the same web UI; semantic search is off there
(full-text only).

1. Share the database folder with the phone: install
   [Syncthing-Fork](https://f-droid.org/packages/com.github.catfriend1.syncthingfork/)
   from F-Droid, add the `~/Sync/glane` folder and set it to **Receive
   Only** on the phone, so the phone can never send a change back.
2. Build and install the app (needs the Android SDK and USB debugging on
   the phone):

   ```sh
   mise run android
   adb install -r android/app/build/outputs/apk/debug/app-debug.apk
   ```

3. Open glane, grant "All files access" (needed to read the Syncthing
   folder), and adjust the database path if it isn't
   `/storage/emulated/0/Sync/glane/glane.db`.

When Syncthing delivers a newer copy, the next search uses it, with no
restart.
````

- [ ] **Step 7: Commit**

```bash
git add android/app/src/main README.md
git commit
```

Message:

```
✨ Add an Android app to search the index offline

The index only lived on the Mac, so nothing could be looked up from a
phone without network. The app is a thin shell: it starts the bundled
glane serve --read-only over the glane.db that Syncthing keeps on the
phone, shows it in a WebView, sends external links to the browser, and
restarts the server when Android kills it in the background.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```
