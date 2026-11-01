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
	if !strings.Contains(string(script), `exec "${SHELL:-/bin/sh}" -lc 'exec glane app'`) {
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
	for _, s := range []string{"Name=Glane", `Exec=sh -c "exec \\"\\${SHELL:-/bin/sh}\\" -lc 'exec glane app'"`, "Icon=glane", "Terminal=false"} {
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
	script := filepath.Join(home, "Applications", "Glane.app", "Contents", "MacOS", "glane")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installLauncher("darwin", home, ""); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("launcher mode = %v, want 0755", fi.Mode().Perm())
	}
}
