# Scheduling

`glane update` is meant to run on a timer. **Scheduled jobs run with a bare
environment** (no shell profile), so you must set the tokens and `GLANE_DB`
explicitly, and use an **absolute path** to the `glane` binary and DB file.

**Create the directories first** — neither launchd nor the cron redirect will
make them, and an explicitly-set `GLANE_DB` parent isn't auto-created either, so
a missing directory means the job silently fails to start:

```sh
mkdir -p ~/.local/share/glane ~/.local/state/glane
```

## macOS (launchd)

Save as `~/Library/LaunchAgents/com.glane.update.plist` (adjust paths, creds,
and the hour):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.glane.update</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/glane</string>
    <string>update</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>GLANE_DB</key><string>/Users/you/.local/share/glane/glane.db</string>
    <key>GITHUB_TOKEN</key><string>ghp_…</string>
    <key>MASTODON_INSTANCE_URL</key><string>https://mastodon.social</string>
    <key>MASTODON_ACCESS_TOKEN</key><string>…</string>
    <key>BLUESKY_HANDLE</key><string>you.bsky.social</string>
    <key>BLUESKY_APP_PASSWORD</key><string>xxxx-xxxx-xxxx-xxxx</string>
    <key>GLANE_EMBED_URL</key><string>http://localhost:11434/v1</string>
    <key>GLANE_EMBED_MODEL</key><string>nomic-embed-text</string>
    <key>GLANE_SUMMARY_URL</key><string>http://localhost:11434/v1</string>
    <key>GLANE_SUMMARY_MODEL</key><string>gemma3</string>
  </dict>
  <key>StartCalendarInterval</key>
  <dict><key>Hour</key><integer>7</integer><key>Minute</key><integer>0</integer></dict>
  <key>StandardOutPath</key><string>/Users/you/.local/state/glane/update.log</string>
  <key>StandardErrorPath</key><string>/Users/you/.local/state/glane/update.log</string>
</dict>
</plist>
```

Load it (and unload to stop):

```sh
launchctl load ~/Library/LaunchAgents/com.glane.update.plist
# launchctl unload ~/Library/LaunchAgents/com.glane.update.plist
```

Progress goes to stderr → the log; the run is idempotent, so a missed run just
catches up next time.

## cron (Linux)

Set the vars in the crontab (cron has no shell profile either), then one line:

```cron
GLANE_DB=/home/you/.local/share/glane/glane.db
GITHUB_TOKEN=ghp_…
MASTODON_INSTANCE_URL=https://mastodon.social
MASTODON_ACCESS_TOKEN=…
BLUESKY_HANDLE=you.bsky.social
BLUESKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
GLANE_EMBED_URL=http://localhost:11434/v1
GLANE_EMBED_MODEL=nomic-embed-text
GLANE_SUMMARY_URL=http://localhost:11434/v1
GLANE_SUMMARY_MODEL=gemma3

0 7 * * * /usr/local/bin/glane update >> /home/you/.local/state/glane/update.log 2>&1
```
