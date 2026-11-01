# Desktop App (`glane app`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `glane app` shows the web UI in a native window that runs its own server on a free port and stops it when the window closes.

**Architecture:** `cmdApp` opens the store, serves `web.Handler` on `127.0.0.1:0` in-process, and opens a [glaze](https://github.com/crgimenes/glaze) window (system WKWebView / WebKitGTK through purego, no cgo). `Run()` returns when the window closes; the server is shut down and the store closed. Links that want a new window go to the default browser.

**Tech Stack:** Go 1.27 (pinned in `mise.toml`), `github.com/crgimenes/glaze v0.0.81`, `github.com/ebitengine/purego` (pulled in by glaze).

**Spec:** `docs/superpowers/specs/2026-11-01-desktop-app-design.md`

## Global Constraints

- Every build stays `CGO_ENABLED=0`; goreleaser builds linux, darwin, windows × amd64, arm64.
- The Android APK cross-compiles the main package with `GOOS=android GOARCH=arm64 CGO_ENABLED=0`; purego doesn't build there, so every file importing glaze carries `//go:build !android`.
- `glane app` has no flags. `--port`, `--read-only`, `GLANE_SERVE_TOKEN` stay `serve`-only.
- Progress/errors to stderr, the final result to stdout (`AGENTS.md`).
- CLI surface changes update `docs/commands.md`, the README command table and `completions/glane.fish` (`AGENTS.md`).
- `go vet ./...` stays clean; `gofmt` clean (`mise run check`).
- Commit subjects follow the repo's gitmoji style (`✨`, `📝`, `♻️`), English, imperative, ≤ 72 chars, body explaining why, last line `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

Run Go commands through `mise exec --` unless mise is activated in your shell.

---

### Task 1: Export the web handler and the app icon

**Files:**
- Modify: `internal/web/web.go:146` (rename `handler` → `Handler`, add `AppIcon`)
- Modify: `internal/web/web.go:244`, `internal/web/readonly.go:94`, `internal/web/web_test.go`, `internal/web/i18n_test.go` (call sites)
- Test: `internal/web/web_test.go`

**Interfaces:**
- Produces: `func Handler(s *store.Store) http.Handler` (same behavior as today's `handler`) and `func AppIcon() []byte` (the embedded `static/icon-512.png`).

- [ ] **Step 1: Rename the handler at every call site**

```sh
perl -pi -e 's/\bhandler\(s\)/Handler(s)/g; s/^func handler\(/func Handler(/' internal/web/*.go
grep -n "handler(" internal/web/*.go   # expect no output
```

Add a doc comment above `func Handler` in `internal/web/web.go`:

```go
// Handler serves the whole web UI over s. Serve, ServeReadOnly and
// `glane app` all mount it.
func Handler(s *store.Store) http.Handler {
```

- [ ] **Step 2: Write the failing test for `AppIcon`**

Append to `internal/web/web_test.go` (add `"bytes"` to its imports):

```go
func TestAppIconIsEmbeddedPNG(t *testing.T) {
	icon := AppIcon()
	if !bytes.HasPrefix(icon, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("AppIcon is not a PNG (%d bytes)", len(icon))
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `mise exec -- go test ./internal/web/ -run TestAppIcon`
Expected: FAIL to compile, `undefined: AppIcon`.

- [ ] **Step 4: Implement `AppIcon`**

In `internal/web/web.go`, after the `assets` declaration:

```go
// AppIcon is the 512 px PNG the manifest uses, for a native window's Dock
// or taskbar icon.
func AppIcon() []byte {
	b, _ := assets.ReadFile("static/icon-512.png") // embedded: can't fail
	return b
}
```

- [ ] **Step 5: Run the package tests**

Run: `mise exec -- go test ./internal/web/ && mise exec -- go vet ./...`
Expected: `ok  github.com/jcgay/glane/internal/web`, vet silent.

- [ ] **Step 6: Commit**

```sh
git add internal/web
git commit -F - <<'EOF'
♻️ Export the web handler and the app icon

A native window needs to mount the same UI as `glane serve` on a listener
of its own, and to show the manifest's icon in the Dock.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 2: `glane app` command

**Files:**
- Create: `app.go` (`//go:build !android`)
- Create: `app_android.go` (`//go:build android`)
- Modify: `main.go:39` (usage line), `main.go:46-50` (dispatch before the shared `store.Open`)
- Modify: `go.mod`, `go.sum` (glaze as a direct dependency)

**Interfaces:**
- Consumes: `web.Handler(s *store.Store) http.Handler`, `web.AppIcon() []byte` (Task 1); `dbPath()`, `fatal(error)` from `main.go`.
- Produces: `func cmdApp(args []string)`.

No automated test: nothing in `cmdApp` runs without a display, and the logic is straight-line wiring. The checks are the cross-builds and the manual run in steps 5–6.

- [ ] **Step 1: Add the dependency**

```sh
mise exec -- go get github.com/crgimenes/glaze@v0.0.81
```

- [ ] **Step 2: Create `app.go`**

```go
//go:build !android

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/crgimenes/glaze"
	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/web"
)

// AppKit (macOS) and GTK (Linux) only run on the process's main thread.
// Pinning the main goroutine to it here, before main runs, is what keeps
// `glane app` from dying with SIGILL; the other commands don't notice.
func init() { runtime.LockOSThread() }

// cmdApp serves the UI on a free loopback port and shows it in a native
// window; closing the window stops the server.
func cmdApp(args []string) {
	if len(args) > 0 {
		fatal(fmt.Errorf("usage: glane app"))
	}
	s, err := store.Open(dbPath())
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	srv := &http.Server{Handler: web.Handler(s), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(l)
	url := "http://" + l.Addr().String() + "/"
	fmt.Printf("glane app serving %s on %s\n", dbPath(), url)

	_ = glaze.SetAppIcon(web.AppIcon()) // unsupported on some platforms: keep the default icon
	w, err := glaze.NewWithOptions(glaze.Options{
		HideUntilLoaded: true,
		OnNewWindow:     openExternal,
	})
	if err != nil {
		fatal(fmt.Errorf("webview unavailable: %w; on Debian/Ubuntu: apt install libwebkit2gtk-4.1-0, or use glane serve", err))
	}
	w.SetTitle("glane")
	w.SetSize(1200, 800, glaze.HintNone)
	w.Navigate(url)
	w.Run()
	w.Destroy()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// openExternal hands a link the page wants in a new window (target=_blank,
// window.open) to the default browser: the app window only shows glane.
func openExternal(url string) {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.Command(opener, url).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "glane: open %s: %v\n", url, err)
	}
}
```

Note: `fatal` calls `os.Exit`, which skips the deferred `s.Close()`. That is
what `cmdServe` already does on errors; SQLite recovers.

- [ ] **Step 3: Create `app_android.go`**

```go
//go:build android

package main

import "errors"

// glaze's purego needs cgo on Android, and the APK has its own UI anyway.
func cmdApp([]string) { fatal(errors.New("glane app is not available on Android")) }
```

- [ ] **Step 4: Wire the command in `main.go`**

Usage line (`main.go:39`):

```go
		fmt.Fprintln(os.Stderr, "usage: glane <import|sync|search|enrich|summarize|update|tags|stats|serve|app|version> ...")
```

Next to the `serve` early dispatch (`main.go:46-50`):

```go
	// serve opens the database itself: --read-only must never create or write it
	if os.Args[1] == "serve" {
		cmdServe(os.Args[2:])
		return
	}
	// app opens the database itself and closes it once the window is gone
	if os.Args[1] == "app" {
		cmdApp(os.Args[2:])
		return
	}
```

- [ ] **Step 5: Build, vet, test, cross-compile**

```sh
mise exec -- go mod tidy
mise exec -- go build -o glane . && mise exec -- go vet ./... && mise exec -- go test ./...
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 android/arm64; do
  printf '%s: ' $t
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 mise exec -- go build -o /dev/null . && echo ok
done
grep glaze go.mod   # expect a direct requirement, no "// indirect"
```

Expected: tests `ok`, vet silent, every target `ok` (android included, thanks to the build tags).

- [ ] **Step 6: Manual check (macOS; Linux too if a desktop is at hand)**

```sh
./glane app
```

Check, then write the results down for the commit body:
1. A "glane" window shows the UI over your database; typing searches, `j`/`k` move.
2. `o` on a result, or a click on its title, opens the link in the default browser; the window stays.
3. `y` copies the URL (paste it somewhere).
4. Close the window with its close button: `./glane app` returns to the prompt, and `pgrep -fl "glane app"` prints nothing.
5. `./glane app extra` prints `glane: usage: glane app` and exits 1.

- [ ] **Step 7: Commit**

```sh
git add app.go app_android.go main.go go.mod go.sum
git commit -F - <<'EOF'
✨ Add glane app, the UI in a native window

The installable web app only lives while `glane serve` does, which meant
a hand-written launchd/systemd unit and a server up all day on port 8080.
`glane app` serves on a free loopback port in-process and stops when its
window closes.

glaze binds the system WKWebView / WebKitGTK through purego, so the build
stays cgo-free. AppKit must run on the main thread, hence the
LockOSThread in init. purego needs cgo on Android, so the command is left
out of the APK's binary.

Checked by hand on macOS; Linux (WebKitGTK) not checked.

https://github.com/crgimenes/glaze

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

Adjust the "checked by hand" line to what step 6 actually covered.

---

### Task 3: Document `glane app`

**Files:**
- Modify: `docs/desktop-app.md` (whole file restructured)
- Modify: `docs/commands.md` (new section after `glane serve`)
- Modify: `README.md:95` (command table), `README.md:130-134` (Desktop app paragraph)
- Modify: `completions/glane.fish:4` and the subcommand list

**Interfaces:**
- Consumes: the behavior of `glane app` from Task 2 (no flags; error text for a missing WebKitGTK).

- [ ] **Step 1: Rewrite the top of `docs/desktop-app.md`**

Replace everything from the start of the file up to (not including) `## Ubuntu (systemd)` with:

````markdown
# Desktop app

`glane app` opens the UI in a window of its own. It starts a server on a free
local port and stops it when you close the window: nothing runs in the
background, nothing to set up at login.

```sh
glane app
```

On Linux it needs WebKitGTK (`apt install libwebkit2gtk-4.1-0` on
Debian/Ubuntu); macOS ships what it needs. Links open in your default
browser.

## Launch it like an app

Started from the Finder, the Dock or the app launcher, a program doesn't see
your shell's environment (`GLANE_DB`, `GLANE_EMBED_*`, `PATH`). The launchers
below go through your login shell, so they see what your terminal sees.

### macOS

```sh
mkdir -p ~/Applications/Glane.app/Contents/MacOS
cat > ~/Applications/Glane.app/Contents/MacOS/glane <<'EOF'
#!/bin/sh
exec "$SHELL" -lc 'glane app'
EOF
chmod +x ~/Applications/Glane.app/Contents/MacOS/glane
cat > ~/Applications/Glane.app/Contents/Info.plist <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key><string>glane</string>
  <key>CFBundleName</key><string>Glane</string>
  <key>CFBundleIdentifier</key><string>com.glane.app</string>
</dict>
</plist>
EOF
```

Open `~/Applications` in the Finder and drag Glane to the Dock.

### Ubuntu

Save as `~/.local/share/applications/glane.desktop`:

```ini
[Desktop Entry]
Type=Application
Name=Glane
Exec=sh -c 'exec "$SHELL" -lc "glane app"'
Terminal=false
```

### When it doesn't start

A launcher has nowhere to print errors. Send them to a file to find out why:
`exec "$SHELL" -lc 'glane app' 2>>/tmp/glane-app.log` (and the same inside
`Exec=` on Ubuntu).

Rather not load your whole shell configuration? Set the variables in the
launcher instead, before `exec`: `export GLANE_EMBED_URL=…`, then call
`glane` by its full path (`command -v glane` prints it).

## Server always on

To keep `glane serve` running instead, for instance to reach it from a
browser tab at any time, the UI also installs as a web app. With
`glane serve` running, open `http://127.0.0.1:8080` and:

- **Chrome / Chromium** (macOS, Ubuntu): menu ⋮ → *Cast, save, and share* →
  *Install page as app…* (or the install icon at the end of the address bar).
- **Safari** (macOS 14+): *File* → *Add to Dock*.

That web app only shows what the server serves, so start `glane serve` at
login. A service doesn't see your shell's environment: put `GLANE_DB`,
`GLANE_EMBED_*` and the rest in the service file itself, and replace the
`glane` path below with the output of `command -v glane` (Homebrew installs
it in `/opt/homebrew/bin` on Apple Silicon, `/home/linuxbrew/.linuxbrew/bin`
on Linux). [Scheduling](scheduling.md) has the same caveats for `glane update`.

````

Then demote the two existing service headings one level so they sit under
"Server always on": `## Ubuntu (systemd)` → `### Ubuntu (systemd)`,
`## macOS (launchd)` → `### macOS (launchd)`.

- [ ] **Step 2: Add the command to `docs/commands.md`**

Append at the end of the file:

````markdown

## `glane app`

Shows the same UI as `glane serve` in a native window (macOS, Linux). It
serves on a free `127.0.0.1` port and stops when the window closes; links
open in your default browser. No flags: for a fixed port or `--read-only`,
use `glane serve`.

Linux needs WebKitGTK (`apt install libwebkit2gtk-4.1-0`); without it the
command exits with that hint. Not available in the Android build. Launching
it from the Dock or the app launcher: [Desktop app](desktop-app.md).
````

- [ ] **Step 3: Update the README**

Command table, after the `glane serve` row (`README.md:95`):

```markdown
| `glane app` | Open the web UI in a native window, with its own server |
```

Replace the "Desktop app" paragraph (`README.md:130-134`) with:

```markdown
## Desktop app

`glane app` opens the UI in its own window and stops its server when you
close it; on Linux it needs WebKitGTK. Dock and launcher setup, and the
always-on alternative: [docs/desktop-app.md](docs/desktop-app.md).
```

- [ ] **Step 4: Update `completions/glane.fish`**

```fish
set -l cmds import sync search serve app enrich summarize update tags stats version
```

and after the `serve` subcommand line:

```fish
complete -c glane -n "not __fish_seen_subcommand_from $cmds" -a app       -d "open the web UI in a native window"
```

- [ ] **Step 5: Check**

```sh
fish -n completions/glane.fish && echo ok
grep -n "glane app" README.md docs/commands.md docs/desktop-app.md completions/glane.fish
grep -n "^## \|^### " docs/desktop-app.md
```

Expected: `ok`; each file mentions `glane app`; headings read Desktop app →
Launch it like an app (macOS, Ubuntu, When it doesn't start) → Server always
on (Ubuntu (systemd), macOS (launchd)).

- [ ] **Step 6: Commit**

```sh
git add docs/desktop-app.md docs/commands.md README.md completions/glane.fish
git commit -F - <<'EOF'
📝 Document glane app and its launchers

A launcher started from the Finder or the app launcher doesn't see the
shell's environment; going through `$SHELL -lc` picks up GLANE_DB and the
embedding settings without copying them. The always-on server with the
installable web app stays documented for those who prefer it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
