# Desktop app

The UI is an installable web app: its own window, an icon in the Dock or the
app launcher, no address bar. With `glane serve` running, open
`http://127.0.0.1:8080` and:

- **Chrome / Chromium** (macOS, Ubuntu): menu ⋮ → *Cast, save, and share* →
  *Install page as app…* (or the install icon at the end of the address bar).
- **Safari** (macOS 14+): *File* → *Add to Dock*.

The app only shows what the server serves, so start `glane serve` at login.
A service doesn't see your shell's environment: put `GLANE_DB`,
`GLANE_EMBED_*` and the rest in the service file itself, and replace the
`glane` path below with the output of `command -v glane` (Homebrew installs
it in `/opt/homebrew/bin` on Apple Silicon, `/home/linuxbrew/.linuxbrew/bin`
on Linux). [Scheduling](scheduling.md) has the same caveats for `glane update`.

## Ubuntu (systemd)

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

## macOS (launchd)

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
