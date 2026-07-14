# glane update + scheduled sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `glane update` (one command running sync all → enrich → summarize over the whole backlog) and document a launchd/cron scheduled entry.

**Architecture:** Refactor `cmdSyncAll` so its core `syncAll(s) bool` returns whether anything failed instead of calling `os.Exit`. Add `enrichAll`/`summarizeAll` that drain the backlog in a single large-limit pass (simple, clog-free, injectable clients for tests). `cmdUpdate` runs the three phases, skips unconfigured pieces, and exits non-zero if any phase hard-failed. Then a README "Scheduling" section.

**Tech Stack:** Go stdlib. No new dependencies.

## Global Constraints

- Module path `github.com/jcgay/glane` (verbatim).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies. No new sync/enrich/summarize logic — only orchestration.
- `update` order is fixed: sync all → enrich → summarize. A failing phase does NOT stop later phases; `update` exits non-zero at the end if any phase hard-failed (so a scheduler can alert).
- Skip unconfigured: sync all already skips connectors without creds; summarize is skipped (not fatal) when `GLANE_SUMMARY_URL` is unset.
- Progress on stderr (reuse `stderrProgress`); phase summary lines on stdout.
- Tests httptest-only (drive injectable clients), no real network.
- Commit: English, leading literal Unicode gitmoji, body explains *why*.

## Drain approach

Both `enrich` and `summarize` drain by processing with a large limit
`const drainLimit = 100000` — practically "everything pending" at personal scale,
in a single pass, so there is no re-loop (and thus no risk of re-attempting a
permanently-failing summarize item or looping forever). Anything beyond
`drainLimit` simply waits for the next scheduled run.

---

### Task 1: `glane update` command

**Files:**
- Modify: `main.go` (refactor `cmdSyncAll`→`syncAll`; add `drainLimit`, `enrichAll`, `summarizeAll`, `cmdUpdate`; add `update` dispatch + usage)
- Test: `main_test.go`

**Interfaces:**
- Produces: `func syncAll(s *store.Store) (failed bool)`; `func enrichAll(s *store.Store, hc *http.Client, emb *embed.Client) (done, failed int, errored bool)`; `func summarizeAll(s *store.Store, c *summarize.Client) (done, failed int)`; `func cmdUpdate(s *store.Store)`.

- [ ] **Step 1: Write the failing tests**

Add to `main_test.go` (package `main`; imports `net/http`, `net/http/httptest`, `testing`, `strings`, and the `store`/`embed`/`summarize` packages as needed):
```go
func TestSyncAllSkipsWhenUnconfigured(t *testing.T) {
	// All connector env empty → every connector skipped → no failure.
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("MASTODON_INSTANCE_URL", "")
	t.Setenv("MASTODON_ACCESS_TOKEN", "")
	t.Setenv("BLUESKY_HANDLE", "")
	t.Setenv("BLUESKY_APP_PASSWORD", "")
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if syncAll(s) {
		t.Fatal("syncAll should report no failure when everything is skipped")
	}
}

// updateServer serves article HTML for any path except /chat/completions,
// which returns an OpenAI-shaped chat response with a JSON summary+tags.
func updateServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"a summary about lambdas\",\"tags\":[\"aws\"]}"}}]}`))
			return
		}
		w.Write([]byte(`<html><head><title>T</title></head><body><article><p>content about aws lambda</p></article></body></html>`))
	}))
}

func TestEnrichAllDrainsBacklog(t *testing.T) {
	srv := updateServer(t)
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.Upsert([]store.Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "a", URL: srv.URL + "/a"},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "b", URL: srv.URL + "/b"},
	})
	done, _, errored := enrichAll(s, srv.Client(), nil)
	if errored || done != 2 {
		t.Fatalf("want 2 enriched, no error; got done=%d errored=%v", done, errored)
	}
	if pend, _ := s.PendingEnrichment(10); len(pend) != 0 {
		t.Fatalf("backlog not drained: %d still pending", len(pend))
	}
}

func TestSummarizeAllDrainsThenFailsSafe(t *testing.T) {
	srv := updateServer(t)
	defer srv.Close()
	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "a", URL: srv.URL + "/a"}})
	// enrich first so there's an article to summarize
	enrichAll(s, srv.Client(), nil)

	c := &summarize.Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	done, failed := summarizeAll(s, c)
	if done != 1 || failed != 0 {
		t.Fatalf("want 1 summarized; got done=%d failed=%d", done, failed)
	}
	if pend, _ := s.PendingSummary(10); len(pend) != 0 {
		t.Fatalf("summary backlog not drained: %d pending", len(pend))
	}

	// Failure path: a bad endpoint must NOT loop forever — single pass returns.
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 500)
	}))
	defer srvBad.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "2", Kind: "like", Text: "b", URL: srv.URL + "/b"}})
	enrichAll(s, srv.Client(), nil)
	cBad := &summarize.Client{BaseURL: srvBad.URL, Model: "m", HTTP: srvBad.Client()}
	d2, f2 := summarizeAll(s, cBad)
	if d2 != 0 || f2 == 0 {
		t.Fatalf("failure path: want 0 done, >0 failed; got done=%d failed=%d", d2, f2)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test . -run 'SyncAll|EnrichAll|SummarizeAll'`
Expected: FAIL — `syncAll`/`enrichAll`/`summarizeAll` undefined.

- [ ] **Step 3: Refactor `cmdSyncAll` into a non-exiting `syncAll`**

In `main.go`, replace `cmdSyncAll` with a thin wrapper over a returning `syncAll`. Keep the existing body verbatim, but move the final `if failed { os.Exit(1) }` out:
```go
func cmdSyncAll(s *store.Store) {
	if syncAll(s) {
		os.Exit(1)
	}
}

// syncAll runs every configured connector (skipping the rest), prints the
// summary line, and returns whether any configured connector failed. It does
// not exit — callers decide (cmdSyncAll exits; cmdUpdate keeps going).
func syncAll(s *store.Store) bool {
	hc := syncClient()
	total := 0
	failed := false
	var ran, skipped []string

	record := func(name string, n int, err error) {
		total += n
		if err != nil {
			failed = true
			fmt.Fprintf(os.Stderr, "glane: %s sync error: %v\n", name, err)
			ran = append(ran, fmt.Sprintf("%s:%d(partial)", name, n))
		} else {
			ran = append(ran, fmt.Sprintf("%s:%d", name, n))
		}
	}

	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		n, err := github.Sync(s, tok, hc, stderrProgress)
		record("github", n, err)
	} else {
		skipped = append(skipped, "github")
	}
	if base, tok := os.Getenv("MASTODON_INSTANCE_URL"), os.Getenv("MASTODON_ACCESS_TOKEN"); base != "" && tok != "" {
		n, err := mastodon.Sync(s, base, tok, hc, stderrProgress)
		record("mastodon", n, err)
	} else {
		skipped = append(skipped, "mastodon")
	}
	if h, pw := os.Getenv("BLUESKY_HANDLE"), os.Getenv("BLUESKY_APP_PASSWORD"); h != "" && pw != "" {
		n, err := bluesky.Sync(s, h, pw, hc, stderrProgress)
		record("bluesky", n, err)
	} else {
		skipped = append(skipped, "bluesky")
	}

	fmt.Printf("synced %d new items [%s]", total, strings.Join(ran, " "))
	if len(skipped) > 0 {
		fmt.Printf(" (skipped, not configured: %s)", strings.Join(skipped, ", "))
	}
	fmt.Println()
	return failed
}
```

- [ ] **Step 4: Add `drainLimit`, `enrichAll`, `summarizeAll`**

Add to `main.go`:
```go
const drainLimit = 100000 // effectively "all pending" at personal scale, single pass

// enrichAll enriches the whole pending backlog in one pass. Injectable clients
// keep it testable. Returns counts and whether enrich.Run hard-errored.
func enrichAll(s *store.Store, hc *http.Client, emb *embed.Client) (int, int, bool) {
	done, failed, err := enrich.Run(s, hc, emb, drainLimit, stderrProgress)
	fmt.Printf("enriched %d, failed %d\n", done, failed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glane: enrich error: %v\n", err)
		return done, failed, true
	}
	return done, failed, false
}

// summarizeAll summarizes the whole pending backlog in one pass (no re-loop, so a
// permanently-failing item can't spin). Failed items stay pending for next run.
func summarizeAll(s *store.Store, c *summarize.Client) (int, int) {
	items, err := s.PendingSummary(drainLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glane: summarize error: %v\n", err)
		return 0, 1 // treat as a failure so update signals it
	}
	done, failed := 0, 0
	for i, it := range items {
		stderrProgress(fmt.Sprintf("summarize [%d/%d]…", i+1, len(items)))
		res, serr := c.Summarize(context.Background(), it.ArticleTitle, it.ArticleText)
		if serr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "glane: summarize item %d: %v\n", it.ID, serr)
			continue
		}
		if serr := s.SaveSummary(it.ID, res.Summary, res.Tags); serr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "glane: summarize save item %d: %v\n", it.ID, serr)
			continue
		}
		done++
	}
	fmt.Printf("summarized %d items (%d failed)\n", done, failed)
	return done, failed
}
```

- [ ] **Step 5: Add `cmdUpdate` and wire the dispatch + usage**

Add `cmdUpdate` and refactor `cmdEnrich`/`cmdSummarize` are left as-is (manual, batch-limited). `cmdUpdate`:
```go
// cmdUpdate runs the full pipeline for a scheduled/one-shot refresh: sync all →
// enrich → summarize. Each phase runs regardless of earlier failures; update
// exits non-zero if any phase hard-failed (for scheduler alerting).
func cmdUpdate(s *store.Store) {
	failed := false
	if syncAll(s) {
		failed = true
	}
	if _, _, errored := enrichAll(s, enrich.DefaultClient(), embed.FromEnv()); errored {
		failed = true
	}
	if c := summarize.FromEnv(); c != nil {
		// A total summarize wipeout (some attempted, none succeeded) signals a
		// down endpoint; a few per-item failures don't trip the exit code.
		done, sfailed := summarizeAll(s, c)
		if done == 0 && sfailed > 0 {
			failed = true
		}
	} else {
		fmt.Println("summarize skipped (GLANE_SUMMARY_URL not set)")
	}
	if failed {
		os.Exit(1)
	}
}
```
Add the dispatch case (alongside the others in the `switch os.Args[1]`):
```go
	case "update":
		cmdUpdate(s)
```
Add `update` to the top-level usage string (the `usage: glane <…> ...` line).

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec -- go test . -v -run 'SyncAll|EnrichAll|SummarizeAll'`
Expected: PASS. Then `mise exec -- go test ./...` — all packages PASS.

- [ ] **Step 7: Manual surface check**

Run:
```bash
mise exec -- go build -o /tmp/glane . && rm -f /tmp/glane-upd.db
GLANE_DB=/tmp/glane-upd.db /tmp/glane import twitter ./twitter >/dev/null
env -u GITHUB_TOKEN -u MASTODON_INSTANCE_URL -u MASTODON_ACCESS_TOKEN -u BLUESKY_HANDLE -u BLUESKY_APP_PASSWORD -u GLANE_SUMMARY_URL \
  GLANE_DB=/tmp/glane-upd.db /tmp/glane update; echo "exit=$?"
```
Expected: prints the sync-all line (everything skipped), an `enriched N, failed M` line (real fetches of imported t.co links — some will fail, fine), and `summarize skipped (GLANE_SUMMARY_URL not set)`. Exit 0 (no configured phase hard-failed). (This does hit the network for enrich — acceptable for a manual check.)

- [ ] **Step 8: Commit**

```bash
git add main.go main_test.go
git commit -m "$(printf '%s' '🔄 Add glane update for one-shot / scheduled refresh

Keeping the veille current took three ordered commands by hand. glane update runs
sync all -> enrich -> summarize in one invocation, draining the whole backlog in
a single large-limit pass (no re-loop, so a permanently-failing summarize item
cant spin). It skips unconfigured pieces and exits non-zero if any phase
hard-fails, so a launchd/cron job can alert. syncAll is refactored to return
failure instead of os.Exit so update runs every phase and still signals failure.')"
```

---

### Task 2: Scheduling documentation

**Files:**
- Modify: `README.md` (add `glane update` to Commands; add a "Scheduling" section)

**Interfaces:** docs only.

- [ ] **Step 1: Add `glane update` to the Commands section**

After the `glane sync all` / before or near `glane serve`, add:
```markdown
### `glane update`
Runs the whole pipeline in one shot — `sync all` → `enrich` → `summarize` —
draining the backlog. Skips unconfigured pieces and exits non-zero if any phase
fails. This is the command to schedule.

```sh
./glane update
```
```

- [ ] **Step 2: Add a "Scheduling" section**

Add near the end (before or after "How it works"):
````markdown
## Scheduling

`glane update` is meant to run on a timer. **Scheduled jobs run with a bare
environment** (no shell profile), so you must set the tokens and `GLANE_DB`
explicitly, and use an **absolute path** to the `glane` binary and DB file.

### macOS (launchd)

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

### cron (Linux)

Set the vars in the crontab (cron has no shell profile either), then one line:

```cron
GLANE_DB=/home/you/.local/share/glane/glane.db
GITHUB_TOKEN=ghp_…
MASTODON_INSTANCE_URL=https://mastodon.social
MASTODON_ACCESS_TOKEN=…
BLUESKY_HANDLE=you.bsky.social
BLUESKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
GLANE_SUMMARY_URL=http://localhost:11434/v1
GLANE_SUMMARY_MODEL=gemma3

0 7 * * * /usr/local/bin/glane update >> /home/you/.local/state/glane/update.log 2>&1
```
````

- [ ] **Step 3: Verify the docs render / links are sane**

Run: `grep -n 'glane update' README.md` and eyeball the new section for correct fenced code blocks (the launchd block is XML inside a ````xml``` fence within a ```` ```` ```` outer block — ensure the section renders; if the nested fences are awkward, use indentation or a single fenced block per snippet).

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "$(printf '%s' '📝 Document glane update and scheduled sync

Add the glane update command and a Scheduling section with a launchd plist and a
cron entry. Both spell out the gotcha that trips people: scheduled jobs run with
a bare environment, so tokens + GLANE_DB and an absolute glane path must be set
explicitly in the plist/crontab.')"
```

---

## Follow-up (out of scope for this plan)

`--quiet`, `sync all` count dedup, tag normalization, and a `summarize_failed`
marker (to skip permanently-failing items across runs) remain optional polish.
