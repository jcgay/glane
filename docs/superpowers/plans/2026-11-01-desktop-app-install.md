# `glane app --install` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `glane app --install` writes a desktop launcher for `glane app`: `~/Applications/Glane.app` on macOS, a `.desktop` entry plus icon on Linux.

**Architecture:** A pure function `installLauncher` in a new `launcher.go` writes the files under the directories it is given; `cmdApp` parses `--install`, calls it with the real home/XDG dirs and prints the paths. The launcher runs `glane app` through the user's login shell so it sees their environment and PATH.

**Tech Stack:** Go 1.27 stdlib only (`encoding/binary`, `os`, `path/filepath`).

**Spec:** `docs/superpowers/specs/2026-11-01-desktop-app-design.md`, section "Addendum : `glane app --install`".

## Global Constraints

- Every build stays `CGO_ENABLED=0`; targets linux, darwin, windows × amd64, arm64, plus `GOOS=android GOARCH=arm64` for the APK. `launcher.go` has no build tag and must not import glaze.
- The launcher calls `glane` from the login shell's PATH, never an absolute path (Homebrew's real path changes on every upgrade).
- macOS bundle identifier `com.glane.app`, name `Glane`, executable `glane`, icon file `glane.icns`, log `$HOME/Library/Logs/glane-app.log`.
- Linux: `$XDG_DATA_HOME` (default `~/.local/share`), files `applications/glane.desktop` and `icons/hicolor/512x512/apps/glane.png`.
- Other OSes: error `--install supports macOS and Linux`.
- Paths written go to stdout; warnings to stderr.
- CLI surface changes update `docs/commands.md`, the README command table and `completions/glane.fish`.
- `go vet ./...` and `gofmt` clean.
- Commits: gitmoji subject, English, imperative, ≤ 72 chars, body explaining why, last line `Co-Authored-By: Claude <model that wrote it> <noreply@anthropic.com>`.

Run Go commands through `mise exec --`.

---

### Task 4: `installLauncher` and the `--install` flag

**Files:**
- Create: `launcher.go`
- Create: `launcher_test.go`
- Modify: `app.go` (flag parsing at the top of `cmdApp`)

**Interfaces:**
- Consumes: `web.AppIcon() []byte` (512 px PNG), `fatal(error)`.
- Produces: `func installLauncher(goos, home, dataHome string) ([]string, error)` returning the absolute paths of the files written; `func icns(png []byte) []byte`.

- [ ] **Step 1: Write the failing tests** — `launcher_test.go`:

```go
package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLauncherDarwin(t *testing.T) {
	home := t.TempDir()
	paths, err := installLauncher("darwin", home, "")
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(home, "Applications", "Glane.app", "Contents")
	want := []string{
		filepath.Join(app, "MacOS", "glane"),
		filepath.Join(app, "Info.plist"),
		filepath.Join(app, "Resources", "glane.icns"),
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	fi, err := os.Stat(want[0])
	if err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("launcher script not executable: %v %v", fi, err)
	}
	script, _ := os.ReadFile(want[0])
	if !strings.Contains(string(script), `exec "$SHELL" -lc 'exec glane app'`) {
		t.Fatalf("script doesn't go through the login shell: %s", script)
	}
	plist, _ := os.ReadFile(want[1])
	for _, s := range []string{"<string>com.glane.app</string>", "<string>glane</string>", "NSHighResolutionCapable"} {
		if !strings.Contains(string(plist), s) {
			t.Errorf("Info.plist lacks %s", s)
		}
	}
	icon, _ := os.ReadFile(want[2])
	if !bytes.HasPrefix(icon, []byte("icns")) || int(binary.BigEndian.Uint32(icon[4:8])) != len(icon) {
		t.Fatalf("bad icns header")
	}
}

func TestInstallLauncherLinux(t *testing.T) {
	data := t.TempDir()
	paths, err := installLauncher("linux", "/unused", data)
	if err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(data, "applications", "glane.desktop")
	png := filepath.Join(data, "icons", "hicolor", "512x512", "apps", "glane.png")
	if strings.Join(paths, "\n") != desktop+"\n"+png {
		t.Fatalf("paths = %v", paths)
	}
	entry, _ := os.ReadFile(desktop)
	for _, s := range []string{"Name=Glane", `Exec=sh -c 'exec "$SHELL" -lc "exec glane app"'`, "Icon=glane", "Terminal=false"} {
		if !strings.Contains(string(entry), s) {
			t.Errorf("glane.desktop lacks %s", s)
		}
	}
	if b, _ := os.ReadFile(png); !bytes.HasPrefix(b, []byte("\x89PNG")) {
		t.Fatal("icon is not a PNG")
	}
}

func TestInstallLauncherOtherOS(t *testing.T) {
	if _, err := installLauncher("windows", t.TempDir(), t.TempDir()); err == nil || err.Error() != "--install supports macOS and Linux" {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallLauncherOverwrites(t *testing.T) {
	home := t.TempDir()
	for range 2 {
		if _, err := installLauncher("darwin", home, ""); err != nil {
			t.Fatal(err)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `mise exec -- go test -run TestInstallLauncher .`
Expected: build failure, `undefined: installLauncher`.

- [ ] **Step 3: Implement `launcher.go`**

```go
package main

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"

	"github.com/jcgay/glane/internal/web"
)

// The launchers go through the login shell: an app started from the Finder,
// the Dock or a desktop launcher doesn't see the user's environment (GLANE_*,
// PATH). They call `glane` by name, never by absolute path, because
// Homebrew's real path (Cellar/glane/<version>/…) changes on every upgrade.
const (
	macLauncher = `#!/bin/sh
exec "$SHELL" -lc 'exec glane app' 2>>"$HOME/Library/Logs/glane-app.log"
`
	macInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key><string>glane</string>
  <key>CFBundleName</key><string>Glane</string>
  <key>CFBundleIdentifier</key><string>com.glane.app</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>glane</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
`
	desktopEntry = `[Desktop Entry]
Type=Application
Name=Glane
Comment=Search your tech-watch
Exec=sh -c 'exec "$SHELL" -lc "exec glane app"'
Icon=glane
Terminal=false
Categories=Utility;
`
)

// installLauncher writes the desktop launcher for `glane app` and returns the
// paths it wrote: ~/Applications/Glane.app on macOS (home), a .desktop entry
// and its icon on Linux (dataHome, the XDG data dir). It overwrites what an
// earlier run wrote.
func installLauncher(goos, home, dataHome string) ([]string, error) {
	type file struct {
		path string
		data []byte
		mode os.FileMode
	}
	var files []file
	switch goos {
	case "darwin":
		app := filepath.Join(home, "Applications", "Glane.app", "Contents")
		files = []file{
			{filepath.Join(app, "MacOS", "glane"), []byte(macLauncher), 0o755},
			{filepath.Join(app, "Info.plist"), []byte(macInfoPlist), 0o644},
			{filepath.Join(app, "Resources", "glane.icns"), icns(web.AppIcon()), 0o644},
		}
	case "linux":
		files = []file{
			{filepath.Join(dataHome, "applications", "glane.desktop"), []byte(desktopEntry), 0o644},
			{filepath.Join(dataHome, "icons", "hicolor", "512x512", "apps", "glane.png"), web.AppIcon(), 0o644},
		}
	default:
		return nil, errors.New("--install supports macOS and Linux")
	}
	var paths []string
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return paths, err
		}
		if err := os.WriteFile(f.path, f.data, f.mode); err != nil {
			return paths, err
		}
		// WriteFile keeps an existing file's mode: make a rewrite executable too
		if err := os.Chmod(f.path, f.mode); err != nil {
			return paths, err
		}
		paths = append(paths, f.path)
	}
	return paths, nil
}

// icns wraps a 512×512 PNG in a one-image Apple icon file: the "icns" header,
// then an "ic09" (512×512) block. Each length is big-endian and counts its
// own 8-byte header.
func icns(png []byte) []byte {
	b := make([]byte, 16, 16+len(png))
	copy(b, "icns")
	binary.BigEndian.PutUint32(b[4:], uint32(16+len(png)))
	copy(b[8:], "ic09")
	binary.BigEndian.PutUint32(b[12:], uint32(8+len(png)))
	return append(b, png...)
}
```

- [ ] **Step 4: Run the tests**

Run: `mise exec -- go test -run TestInstallLauncher -v .`
Expected: the four tests PASS.

- [ ] **Step 5: Wire `--install` into `cmdApp`** — in `app.go`, replace

```go
	if len(args) > 0 {
		fatal(fmt.Errorf("usage: glane app"))
	}
```

with

```go
	fs := flag.NewFlagSet("app", flag.ExitOnError)
	install := fs.Bool("install", false, "write a desktop launcher (macOS: ~/Applications/Glane.app) instead of opening the window")
	fs.Parse(args)
	if fs.NArg() > 0 {
		fatal(fmt.Errorf("usage: glane app [--install]"))
	}
	if *install {
		cmdInstallLauncher()
		return
	}
```

and add `"flag"` to its imports. Then append to `launcher.go` (add `"fmt"`, `"os/exec"`, `"runtime"` to its imports):

```go
func cmdInstallLauncher() {
	home, err := os.UserHomeDir()
	if err != nil {
		fatal(err)
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	paths, err := installLauncher(runtime.GOOS, home, dataHome)
	if err != nil {
		fatal(err)
	}
	for _, p := range paths {
		fmt.Println(p)
	}
	if _, err := exec.LookPath("glane"); err != nil {
		fmt.Fprintln(os.Stderr, "glane: warning: glane is not on your PATH; the launcher runs `glane app` from your login shell")
	}
}
```

Note: `launcher.go` has no build tag, but `cmdInstallLauncher` is only called from `app.go` (`!android`). On Android it is dead code; `go vet` accepts unused unexported functions.

Update the usage line in `main.go`? No: it lists commands, not flags.

- [ ] **Step 6: Full check**

```sh
mise exec -- gofmt -l .        # expect nothing
mise exec -- go vet ./... && mise exec -- go test ./...
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 android/arm64; do
  printf '%s: ' $t; mise exec -- env GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null . && echo ok
done
mise exec -- go build -o glane .
HOME=$(mktemp -d) ./glane app --install   # prints 3 paths under the temp HOME
./glane app extra; echo "exit $?"          # glane: usage: glane app [--install] / exit 1
```

Do NOT run `./glane app` without `--install` (it opens a window).

- [ ] **Step 7: Commit**

```sh
git add launcher.go launcher_test.go app.go
git commit -F - <<'EOF'
✨ Add glane app --install to create the desktop launcher

Writing Glane.app or the .desktop entry by hand meant copying heredocs.
A Homebrew cask can't ship it either: app bundles need GoReleaser Pro,
and a downloaded unsigned app is quarantined by Gatekeeper. Files the
binary writes locally carry no quarantine attribute.

The launcher calls `glane` through the login shell's PATH rather than by
absolute path, because Homebrew's real path changes on every upgrade.
The icns is built from the embedded PNG so nothing depends on sips.

Co-Authored-By: Claude <model> <noreply@anthropic.com>
EOF
```

---

### Task 5: Document `--install`

**Files:**
- Modify: `docs/desktop-app.md` (section "Launch it like an app")
- Modify: `docs/commands.md` (section `glane app`)
- Modify: `README.md` (command table row)
- Modify: `completions/glane.fish`
- Modify: `.goreleaser.yaml` (`brews[0].caveats`)

- [ ] **Step 1: `docs/desktop-app.md`** — replace everything from `## Launch it like an app` up to (not including) `## Server always on` with:

````markdown
## Launch it like an app

```sh
glane app --install
```

writes a launcher: `~/Applications/Glane.app` on macOS (drag it to the
Dock), a `Glane` entry in the app launcher on Linux. Run it again after an
upgrade if the icon changed; to remove it, delete the paths it printed.

Started from the Finder, the Dock or the app launcher, a program doesn't see
your shell's environment (`GLANE_DB`, `GLANE_EMBED_*`, `PATH`). The launcher
goes through your login shell (`$SHELL -lc 'glane app'`), so it sees what
your terminal sees, and finds `glane` on your PATH.

### When it doesn't start

On macOS, errors go to `~/Library/Logs/glane-app.log` (Console.app shows
it). On Linux, `journalctl --user -e` shows them.

Rather not load your whole shell configuration? Edit the launcher
(`Glane.app/Contents/MacOS/glane`, or the `Exec=` line of
`glane.desktop`): set the variables before `exec`, then call `glane` by its
full path (`command -v glane` prints it). `--install` overwrites these edits.

````

- [ ] **Step 2: `docs/commands.md`** — the `## \`glane app\`` heading becomes `## \`glane app [--install]\``, and replace `No flags: for a fixed port or \`--read-only\`,\nuse \`glane serve\`.` with:

```markdown
For a fixed port or `--read-only`, use `glane serve`.

- **`--install`** writes a launcher instead of opening the window:
  `~/Applications/Glane.app` on macOS; `glane.desktop` and its icon under
  `$XDG_DATA_HOME` (`~/.local/share`) on Linux. It prints the paths, and
  overwrites an earlier install. The launcher runs `glane app` through your
  login shell, so it needs `glane` on that shell's PATH.
```

- [ ] **Step 3: README** — the command table row becomes:

```markdown
| `glane app [--install]` | Open the web UI in a native window, with its own server; `--install` adds it to the Dock / app launcher |
```

- [ ] **Step 4: `completions/glane.fish`** — after the `serve` flag lines add:

```fish
complete -c glane -n "__fish_seen_subcommand_from app"       -l install -d "write a desktop launcher (Glane.app / .desktop)"
```

- [ ] **Step 5: `.goreleaser.yaml`** — `brews[0].caveats` becomes:

```yaml
    caveats: |
      Desktop app (Dock / app launcher): glane app --install

      To let Claude Code search your tech-watch, register the plugin:
        claude plugin marketplace add jcgay/glane
        claude plugin install glane@glane
```

- [ ] **Step 6: Check**

```sh
fish -n completions/glane.fish && echo ok
grep -n -- "--install" README.md docs/commands.md docs/desktop-app.md completions/glane.fish .goreleaser.yaml
grep -n "^## \|^### " docs/desktop-app.md
```

Expected: `ok`; every file mentions `--install`; headings: Desktop app → Launch it like an app → When it doesn't start → Server always on → Ubuntu (systemd) → macOS (launchd).

- [ ] **Step 7: Commit**

```sh
git add docs/desktop-app.md docs/commands.md README.md completions/glane.fish .goreleaser.yaml
git commit -F - <<'EOF'
📝 Document glane app --install

The launcher setup shrinks from two pages of heredocs to one command;
the Homebrew caveats point to it, since brew can't install the app
bundle itself.

Co-Authored-By: Claude <model> <noreply@anthropic.com>
EOF
```
