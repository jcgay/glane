# Bluesky External Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture a Bluesky post's external link (from `record.embed`) into the item's `Text`, so `enrich` fetches the real article and its title/description are searchable.

**Architecture:** One change in `internal/bluesky/bluesky.go`: decode `post.embed` (a `$type`-discriminated union), and in the shared `postView.toItem`, append an external link's `title`/`description`/`uri` to `Text`. No enrich/schema/store change; covers all four Bluesky streams via the shared mapper.

**Tech Stack:** Go stdlib (`strings`, already imported). No new dependencies.

## Global Constraints

- Module path `github.com/jcgay/glane` (verbatim).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies — stdlib only. No schema/`enrich`/store change.
- The external URI must appear **literally** (untruncated) in `Text` so `enrich.FirstURL(it.Text)` finds it. `it.URL` stays the `bsky.app` permalink.
- Verified shapes: `embed.$type == "app.bsky.embed.external#view"` → `embed.external.{uri,title,description}`; `embed.$type == "app.bsky.embed.recordWithMedia#view"` → `embed.media` (an `external#view`) → `embed.media.external.{...}`. Other embed types → no external link.
- Tests httptest/unit only — no real network.
- Commit: English, leading literal Unicode gitmoji, body explains *why*.

---

### Task 1: Extract external links in the Bluesky mapper

**Files:**
- Modify: `internal/bluesky/bluesky.go` (add embed types + `Embed` field on `postView`; `externalLink` helper; extend `toItem`)
- Test: `internal/bluesky/bluesky_test.go`

**Interfaces:**
- Consumes: existing `postView`, `store.Item`.
- Produces (unexported): `type externalView struct{ URI, Title, Description string }`, `type embedMediaView`, `type embedView`, `func externalLink(*embedView) *externalView`, `func joinNonEmpty(...string) string`. `postView` gains `Embed *embedView`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/bluesky/bluesky_test.go` (uses `strings` — already imported there):
```go
func TestToItemAppendsExternalCard(t *testing.T) {
	var p postView
	p.URI = "at://d/app.bsky.feed.post/x"
	p.Author.Handle = "a.bsky.social"
	p.Record.Text = "great read"
	p.Embed = &embedView{
		Type:     "app.bsky.embed.external#view",
		External: &externalView{URI: "https://example.com/article", Title: "Scaling Postgres", Description: "index tips"},
	}
	it := p.toItem("like")
	for _, want := range []string{"great read", "Scaling Postgres", "index tips", "https://example.com/article"} {
		if !strings.Contains(it.Text, want) {
			t.Fatalf("Text %q missing %q", it.Text, want)
		}
	}
	// URL stays the permalink, not the external link
	if it.URL != "https://bsky.app/profile/a.bsky.social/post/x" {
		t.Fatalf("URL should stay the permalink, got %q", it.URL)
	}
}

func TestToItemRecordWithMediaExternal(t *testing.T) {
	var p postView
	p.Record.Text = "quoting this"
	p.Embed = &embedView{
		Type:  "app.bsky.embed.recordWithMedia#view",
		Media: &embedMediaView{Type: "app.bsky.embed.external#view", External: &externalView{URI: "https://x.com/m", Title: "Media Title"}},
	}
	it := p.toItem("like")
	if !strings.Contains(it.Text, "https://x.com/m") || !strings.Contains(it.Text, "Media Title") {
		t.Fatalf("recordWithMedia external not appended: %q", it.Text)
	}
}

func TestToItemNoExternalLeavesTextAlone(t *testing.T) {
	var p postView
	p.Record.Text = "just text"
	p.Embed = &embedView{Type: "app.bsky.embed.images#view"} // no external
	if got := p.toItem("own").Text; got != "just text" {
		t.Fatalf("images embed changed text: %q", got)
	}
	p.Embed = nil // no embed at all
	if got := p.toItem("own").Text; got != "just text" {
		t.Fatalf("nil embed changed text: %q", got)
	}
}

func TestSyncExternalLinkIsSearchableAndInText(t *testing.T) {
	feedPost := `{"post":{"uri":"at://did:plc:abc/app.bsky.feed.post/e1","author":{"handle":"bob.bsky.social"},"record":{"text":"great read","createdAt":"2023-05-01T00:00:00Z"},"embed":{"$type":"app.bsky.embed.external#view","external":{"uri":"https://example.com/scaling-postgres","title":"Scaling Postgres","description":"index tips"}}}}`
	srv := newServer(t, serverPages{likes: map[string]string{"": `{"feed":[` + feedPost + `]}`}})
	defer srv.Close()
	pdsBase = srv.URL
	defer func() { pdsBase = "https://bsky.social" }()

	s, _ := store.Open(t.TempDir() + "/t.db")
	defer s.Close()
	if _, err := Sync(s, "alice.bsky.social", "pw", srv.Client()); err != nil {
		t.Fatal(err)
	}
	res, _ := s.SearchFTS("Postgres", store.Filter{}) // article title is now searchable
	if len(res) != 1 {
		t.Fatalf("article title not searchable: %d hits", len(res))
	}
	if !strings.Contains(res[0].Text, "https://example.com/scaling-postgres") {
		t.Fatalf("external URL not in Text (enrich could not follow it): %q", res[0].Text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./internal/bluesky/ -run 'External|RecordWithMedia|NoExternal'`
Expected: FAIL — `embedView`/`externalView` undefined; `postView` has no `Embed`.

- [ ] **Step 3: Add the embed types + `Embed` field + helpers**

In `internal/bluesky/bluesky.go`, add near `postView`:
```go
type externalView struct {
	URI         string `json:"uri"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type embedMediaView struct {
	Type     string        `json:"$type"`
	External *externalView `json:"external"`
}

type embedView struct {
	Type     string          `json:"$type"`
	External *externalView   `json:"external"` // app.bsky.embed.external#view
	Media    *embedMediaView `json:"media"`    // app.bsky.embed.recordWithMedia#view
}

// externalLink returns the shared external link of a post embed, if any —
// either a direct external card or the external media of a quote-with-link.
func externalLink(e *embedView) *externalView {
	if e == nil {
		return nil
	}
	if e.Type == "app.bsky.embed.external#view" && e.External != nil {
		return e.External
	}
	if e.Type == "app.bsky.embed.recordWithMedia#view" && e.Media != nil &&
		e.Media.Type == "app.bsky.embed.external#view" && e.Media.External != nil {
		return e.Media.External
	}
	return nil
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, " ")
}
```
Add the field to `postView`:
```go
	Embed *embedView `json:"embed"`
```

- [ ] **Step 4: Fold the external link into `toItem`**

Change `postView.toItem` so `Text` includes the external link when present:
```go
func (p postView) toItem(kind string) store.Item {
	var ts int64
	if t, err := time.Parse(time.RFC3339, p.Record.CreatedAt); err == nil {
		ts = t.Unix()
	}
	text := p.Record.Text
	if ext := externalLink(p.Embed); ext != nil {
		// Append the shared article's title/description/URL so enrich's
		// FirstURL(Text) can follow it and FTS indexes the article context.
		text = joinNonEmpty(text, ext.Title, ext.Description, ext.URI)
	}
	return store.Item{
		Source:    "bluesky",
		SourceID:  p.URI,
		Kind:      kind,
		Author:    p.Author.Handle,
		Text:      text,
		URL:       permalink(p.Author.Handle, p.URI),
		CreatedAt: ts,
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/bluesky/ -v`
Expected: PASS — the four new tests plus all existing bluesky tests (which have no embed → `Text` unchanged).

- [ ] **Step 6: Full regression**

Run: `mise exec -- go build ./... && mise exec -- go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bluesky/
git commit -m "$(printf '%s' '🔗 Capture Bluesky external links for enrichment

A shared article URL lives in record.embed, not record.text, so enrich never saw
it and fell back to the un-extractable bsky.app permalink. Decode the embed
(external#view card, or a recordWithMedia#view whose media is external) and
append the link title/description/uri to the item Text — so enrich FirstURL
follows the real article and FTS indexes its context. One shared toItem change
covers likes, saved posts, own posts, and reposts alike.')"
```

---

## Follow-up (out of scope for this plan)

Downloading the embed thumbnail, and the other tracked ideas (clickable web tags, `--quiet`, `sync all` count dedup, tag normalization, a documented cron entry), remain separate.
