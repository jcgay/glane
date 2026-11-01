package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/jcgay/glane/internal/web"
)

// The launchers go through the login shell: an app started from the Finder,
// the Dock or a desktop launcher doesn't see the user's environment (GLANE_*,
// PATH). They call `glane` by name, never by absolute path, because
// Homebrew's real path (Cellar/glane/<version>/…) changes on every upgrade.
const (
	macLauncher = `#!/bin/sh
exec "${SHELL:-/bin/sh}" -lc 'exec glane app' 2>>"$HOME/Library/Logs/glane-app.log"
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
Exec=sh -c "exec \\"\\${SHELL:-/bin/sh}\\" -lc 'exec glane app'"
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
