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

A launcher has nowhere to print errors. Send them to a file to find out why,
on macOS:

```sh
exec "$SHELL" -lc 'glane app' 2>>/tmp/glane-app.log
```

and on Ubuntu:

```ini
Exec=sh -c 'exec "$SHELL" -lc "glane app" 2>>/tmp/glane-app.log'
```

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

### Ubuntu (systemd)

Save as `~/.config/systemd/user/glane.service`:

```ini
[Unit]
Description=glane web UI

[Service]
ExecStart=/home/linuxbrew/.linuxbrew/bin/glane serve
# Environment=GLANE_EMBED_URL=http://localhost:11434/v1
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user enable --now glane
journalctl --user -u glane   # why it doesn't start
```

### macOS (launchd)

Save as `~/Library/LaunchAgents/com.glane.serve.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.glane.serve</string>
  <key>ProgramArguments</key>
  <array><string>/opt/homebrew/bin/glane</string><string>serve</string></array>
  <!-- <key>EnvironmentVariables</key>
  <dict><key>GLANE_EMBED_URL</key><string>http://localhost:11434/v1</string></dict> -->
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/Users/you/.local/state/glane/serve.log</string>
  <key>StandardErrorPath</key><string>/Users/you/.local/state/glane/serve.log</string>
</dict>
</plist>
```

```sh
mkdir -p ~/.local/state/glane   # launchd won't create the log directory
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.glane.serve.plist
```

To stop it, or before bootstrapping again after editing the plist:

```sh
launchctl bootout gui/$(id -u)/com.glane.serve
```
