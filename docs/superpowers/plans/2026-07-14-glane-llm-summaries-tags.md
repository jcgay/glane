# LLM Summaries + Tags Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `glane summarize` — one optional LLM call per enriched article that produces a readable summary and free topic tags — plus `glane tags` and a `--tag` search filter.

**Architecture:** `internal/summarize` is a chat-completions client returning `{Summary, Tags}` from one call. The store gains an `item_tags` table (new table → no FTS migration), `PendingSummary`/`SaveSummary`/`TagCounts`/`ByTag`/`TagsFor` helpers, and a `Filter.Tag` field threaded through both `SearchFTS` and `AllEmbeddings` (so hybrid search stays consistent, exactly like `--source`/`--since`). `main.go`/`web` gain `summarize`, `tags`, `--tag`, and show the summary + tags in results.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`, `strings`, `context`, `time`), existing `store`. No new dependencies.

## Global Constraints

- Module path: `github.com/jcgay/glane` (verbatim in every import).
- Toolchain via mise; run go as `mise exec -- go ...`.
- No new dependencies — stdlib only.
- Optional layer: with `GLANE_SUMMARY_URL` unset, nothing changes and `search`/`serve`/`enrich`/`sync` behave as before. `glane summarize` with it unset → clear fatal.
- Config trio: `GLANE_SUMMARY_URL` + `GLANE_SUMMARY_MODEL` + `GLANE_SUMMARY_KEY` (chat/completions endpoint).
- The summary is ADDITIVE: never replace `article_text` in FTS, and do NOT re-embed from the summary — embeddings stay untouched.
- `summarize` is resumable/idempotent (empty `article_summary` = not done) and per-item errors are logged + skipped, never fatal to the batch.
- Tags: lowercased, trimmed, deduped, capped at 6. Free-form (no fixed taxonomy).
- Commit messages: English, leading literal Unicode gitmoji character (not a `:shortcode:`), body explains *why*.

---

### Task 1: Summarize client

**Files:**
- Create: `internal/summarize/summarize.go`
- Test: `internal/summarize/summarize_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  - `type Result struct { Summary string; Tags []string }`
  - `type Client struct { BaseURL, Model, APIKey string; HTTP *http.Client }`
  - `func FromEnv() *Client` — nil if `GLANE_SUMMARY_URL` unset.
  - `func (c *Client) Summarize(ctx context.Context, title, article string) (Result, error)`.

- [ ] **Step 1: Write the failing test**

`internal/summarize/summarize_test.go`:
```go
package summarize

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func chatResponse(content string) string {
	b, _ := jsonMarshalString(content)
	return fmt.Sprintf(`{"choices":[{"message":{"content":%s}}]}`, b)
}

// jsonMarshalString quotes a string as a JSON string literal (test helper).
func jsonMarshalString(s string) (string, error) {
	out := `"`
	for _, r := range s {
		switch r {
		case '"':
			out += `\"`
		case '\\':
			out += `\\`
		case '\n':
			out += `\n`
		default:
			out += string(r)
		}
	}
	return out + `"`, nil
}

func TestSummarizeParsesPlainJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse(`{"summary":"It is about lambda cold starts.","tags":["AWS","lambda","lambda"]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	res, err := c.Summarize(context.Background(), "Title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "It is about lambda cold starts." {
		t.Fatalf("summary %q", res.Summary)
	}
	// lowercased + deduped
	if len(res.Tags) != 2 || res.Tags[0] != "aws" || res.Tags[1] != "lambda" {
		t.Fatalf("tags %v", res.Tags)
	}
}

func TestSummarizeExtractsFencedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse("Sure!\n```json\n{\"summary\":\"S\",\"tags\":[\"go\"]}\n```"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	res, err := c.Summarize(context.Background(), "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "S" || len(res.Tags) != 1 || res.Tags[0] != "go" {
		t.Fatalf("got %+v", res)
	}
}

func TestSummarizeErrorsOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	if _, err := c.Summarize(context.Background(), "t", "b"); err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `mise exec -- go test ./internal/summarize/`
Expected: FAIL — `undefined: Client` / build error.

- [ ] **Step 3: Write `internal/summarize/summarize.go`**

```go
package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Result struct {
	Summary string
	Tags    []string
}

type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

func FromEnv() *Client {
	url := os.Getenv("GLANE_SUMMARY_URL")
	if url == "" {
		return nil
	}
	return &Client{
		BaseURL: url,
		Model:   os.Getenv("GLANE_SUMMARY_MODEL"),
		APIKey:  os.Getenv("GLANE_SUMMARY_KEY"),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

const systemPrompt = `You summarize and categorize technical articles. Reply with ONLY a JSON object, no prose and no code fences: {"summary": "a 2-3 sentence summary for a technical reader focusing on the key takeaway", "tags": ["3-6 lowercase topic or technology tags"]}`

func (c *Client) Summarize(ctx context.Context, title, article string) (Result, error) {
	body, _ := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": title + "\n\n" + cutRunes(article, 8000)},
		},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("summary: status %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, err
	}
	if len(out.Choices) == 0 {
		return Result{}, fmt.Errorf("summary: no choices")
	}
	var parsed struct {
		Summary string   `json:"summary"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(extractJSON(out.Choices[0].Message.Content)), &parsed); err != nil {
		return Result{}, fmt.Errorf("summary: bad JSON: %w", err)
	}
	if parsed.Summary == "" {
		return Result{}, fmt.Errorf("summary: empty summary")
	}
	return Result{Summary: parsed.Summary, Tags: cleanTags(parsed.Tags)}, nil
}

// extractJSON returns the substring from the first '{' to the last '}', so a
// model that wraps the object in prose or ```json fences still parses.
func extractJSON(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/summarize/ -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/summarize/
git commit -m "$(printf '%s' '🤖 Add LLM summarize+tag client

One chat call returns both a readable summary and free topic tags as a JSON
object, so categorizing costs no extra request. The parser is lenient (first {
to last }) because instruction-tuned models often wrap JSON in prose or code
fences, and tags are lowercased/deduped/capped so the vocabulary stays usable.
Article input is rune-truncated to bound token cost.')"
```

---

### Task 2: Store — tags table + summary/tag queries

**Files:**
- Modify: `internal/store/store.go` (add `Tags` field to `Item`; add `item_tags` table + index to `schema`)
- Modify: `internal/store/search.go` (add `Filter.Tag`; thread into `SearchFTS`; add `ByTag`)
- Modify: `internal/store/vector.go` (thread `Filter.Tag` into `AllEmbeddings`)
- Create: `internal/store/summary.go` (`PendingSummary`, `SaveSummary`, `TagCounts`, `TagsFor`, `AttachTags`)
- Test: `internal/store/summary_test.go`

**Interfaces:**
- Consumes: existing `Store`, `Item`, `Filter`, `Result`.
- Produces:
  - `Item.Tags []string` (populated on read, not a column).
  - `Filter.Tag string`.
  - `func (s *Store) PendingSummary(limit int) ([]Item, error)` — items with `article_text != '' AND article_summary = '' AND fetch_status = 'ok'`; each Item carries `ID`, `ArticleTitle`, `ArticleText`.
  - `func (s *Store) SaveSummary(id int64, summary string, tags []string) error`.
  - `type TagCount struct { Tag string; Count int }` and `func (s *Store) TagCounts() ([]TagCount, error)`.
  - `func (s *Store) TagsFor(ids []int64) (map[int64][]string, error)`.
  - `func (s *Store) AttachTags(results []Result) error`.
  - `func (s *Store) ByTag(tag string, f Filter) ([]Result, error)`.

- [ ] **Step 1: Add the `item_tags` table and the `Item.Tags` field**

In `internal/store/store.go`, append to the `schema` const (after `sync_state`):
```sql

CREATE TABLE IF NOT EXISTS item_tags (
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  PRIMARY KEY (item_id, tag)
);
CREATE INDEX IF NOT EXISTS item_tags_tag ON item_tags(tag);
```
Add a field to the `Item` struct (with the other read-populated fields):
```go
	Tags []string // populated on read from item_tags, not a column
```

- [ ] **Step 2: Add `Filter.Tag` and write the failing store test**

In `internal/store/search.go`, add `Tag string` to the `Filter` struct.

`internal/store/summary_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func seedEnriched(t *testing.T, s *Store, id int, src, txt string) {
	t.Helper()
	if _, err := s.Upsert([]Item{{Source: src, SourceID: itoa(id), Kind: "like", Text: txt, URL: "http://x/" + itoa(id)}}); err != nil {
		t.Fatal(err)
	}
	// mark it enriched so PendingSummary picks it up
	var rowid int64
	s.db.QueryRow(`SELECT id FROM items WHERE source=? AND source_id=?`, src, itoa(id)).Scan(&rowid)
	if _, err := s.db.Exec(`UPDATE items SET article_text=?, fetch_status='ok' WHERE id=?`, "article "+txt, rowid); err != nil {
		t.Fatal(err)
	}
}

func itoa(i int) string { return string(rune('0' + i)) }

func TestSummaryAndTagsRoundTrip(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	seedEnriched(t, s, 1, "twitter", "kube post")

	pend, err := s.PendingSummary(10)
	if err != nil || len(pend) != 1 {
		t.Fatalf("pending=%d err=%v", len(pend), err)
	}
	if err := s.SaveSummary(pend[0].ID, "a summary about kubernetes", []string{"kubernetes", "devops"}); err != nil {
		t.Fatal(err)
	}
	// no longer pending
	if p, _ := s.PendingSummary(10); len(p) != 0 {
		t.Fatalf("still pending: %d", len(p))
	}
	// summary searchable via FTS
	if res, _ := s.SearchFTS("kubernetes", Filter{}); len(res) != 1 {
		t.Fatalf("summary not FTS-indexed")
	}
	// tag counts
	tc, _ := s.TagCounts()
	if len(tc) != 2 {
		t.Fatalf("tag counts %+v", tc)
	}
	// TagsFor
	m, _ := s.TagsFor([]int64{pend[0].ID})
	if len(m[pend[0].ID]) != 2 {
		t.Fatalf("tagsfor %+v", m)
	}
}

func TestTagFilterConstrainsFTS(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	seedEnriched(t, s, 1, "twitter", "alpha")
	seedEnriched(t, s, 2, "github", "alpha")
	// tag only item 1
	var id1 int64
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(1)).Scan(&id1)
	s.SaveSummary(id1, "s1", []string{"rust"})
	var id2 int64
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(2)).Scan(&id2)
	s.SaveSummary(id2, "s2", []string{"go"})

	res, err := s.SearchFTS("alpha", Filter{Tag: "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != id1 {
		t.Fatalf("tag filter failed: %+v", res)
	}

	// ByTag browse (no query)
	bt, _ := s.ByTag("go", Filter{})
	if len(bt) != 1 || bt[0].ID != id2 {
		t.Fatalf("ByTag failed: %+v", bt)
	}
}

func TestTagFilterConstrainsEmbeddings(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	seedEnriched(t, s, 1, "twitter", "alpha")
	seedEnriched(t, s, 2, "github", "beta")
	var id1, id2 int64
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(1)).Scan(&id1)
	s.db.QueryRow(`SELECT id FROM items WHERE source_id=?`, itoa(2)).Scan(&id2)
	s.SaveSummary(id1, "s1", []string{"rust"})
	s.SaveSummary(id2, "s2", []string{"go"})
	if err := s.SaveEmbedding(id1, "m", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEmbedding(id2, "m", []float32{0, 1}); err != nil {
		t.Fatal(err)
	}

	if all, _ := s.AllEmbeddings("m", Filter{}); len(all) != 2 {
		t.Fatalf("want 2 embeddings unfiltered, got %d", len(all))
	}
	tagged, err := s.AllEmbeddings("m", Filter{Tag: "rust"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tagged) != 1 || tagged[0].ID != id1 {
		t.Fatalf("tag filter on embeddings failed: %+v", tagged)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `mise exec -- go test ./internal/store/ -run 'Summary|Tag'`
Expected: FAIL — `undefined: (*Store).PendingSummary` etc.

- [ ] **Step 4: Write `internal/store/summary.go`**

```go
package store

import "strings"

func (s *Store) PendingSummary(limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, article_title, article_text
		FROM items
		WHERE article_text != '' AND article_summary = '' AND fetch_status = 'ok'
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.ArticleTitle, &it.ArticleText); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) SaveSummary(id int64, summary string, tags []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE items SET article_summary=? WHERE id=?`, summary, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM item_tags WHERE item_id=?`, id); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO item_tags (item_id, tag) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, tag := range tags {
		if _, err := stmt.Exec(id, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type TagCount struct {
	Tag   string
	Count int
}

func (s *Store) TagCounts() ([]TagCount, error) {
	rows, err := s.db.Query(`SELECT tag, COUNT(*) c FROM item_tags GROUP BY tag ORDER BY c DESC, tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (s *Store) TagsFor(ids []int64) (map[int64][]string, error) {
	m := map[int64][]string{}
	if len(ids) == 0 {
		return m, nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT item_id, tag FROM item_tags WHERE item_id IN (`+ph+`) ORDER BY tag`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, err
		}
		m[id] = append(m[id], tag)
	}
	return m, rows.Err()
}

// AttachTags fills each result's Tags from item_tags in one query.
func (s *Store) AttachTags(results []Result) error {
	ids := make([]int64, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	m, err := s.TagsFor(ids)
	if err != nil {
		return err
	}
	for i := range results {
		results[i].Tags = m[results[i].ID]
	}
	return nil
}
```

- [ ] **Step 5: Thread `Filter.Tag` into `SearchFTS` and add `ByTag`**

In `internal/store/search.go`'s `SearchFTS`, after the `f.Since` clause and before the `ORDER BY`, add:
```go
	if f.Tag != "" {
		sql += " AND EXISTS (SELECT 1 FROM item_tags t WHERE t.item_id = i.id AND t.tag = ?)"
		args = append(args, f.Tag)
	}
```
Add `ByTag` to `internal/store/search.go`:
```go
// ByTag lists items carrying a tag, newest first — for `search --tag X` with no
// text query (no FTS MATCH involved).
func (s *Store) ByTag(tag string, f Filter) ([]Result, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	sql := `
		SELECT i.id, i.source, i.source_id, i.kind, i.author, i.text, i.url,
		       i.created_at, i.link_url, i.article_title, i.article_summary
		FROM item_tags t JOIN items i ON i.id = t.item_id
		WHERE t.tag = ?`
	args := []any{tag}
	if f.Source != "" {
		sql += " AND i.source = ?"
		args = append(args, f.Source)
	}
	if f.Since > 0 {
		sql += " AND i.created_at >= ?"
		args = append(args, f.Since)
	}
	sql += " ORDER BY i.created_at DESC LIMIT ?"
	args = append(args, f.Limit)

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Source, &r.SourceID, &r.Kind, &r.Author,
			&r.Text, &r.URL, &r.CreatedAt, &r.LinkURL, &r.ArticleTitle,
			&r.ArticleSummary); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Thread `Filter.Tag` into `AllEmbeddings`**

In `internal/store/vector.go`'s `AllEmbeddings` (which already `JOIN items i ON i.id = e.item_id` and filters source/since), add after the `f.Since` clause:
```go
	if f.Tag != "" {
		sql += " AND EXISTS (SELECT 1 FROM item_tags t WHERE t.item_id = i.id AND t.tag = ?)"
		args = append(args, f.Tag)
	}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `mise exec -- go test ./internal/store/ -v`
Expected: PASS (new Summary/Tag tests + all existing store tests).

- [ ] **Step 8: Commit**

```bash
git add internal/store/
git commit -m "$(printf '%s' '🏷️ Store item tags + summary/tag queries

Tags live in a new item_tags table created with IF NOT EXISTS, so existing
databases need no FTS rebuild or migration. SaveSummary writes the summary and
replaces the item tags in one transaction. Filter.Tag threads through both
SearchFTS and AllEmbeddings so a --tag filter constrains full-text and semantic
candidates alike — the same consistency the --source filter already has, so
hybrid search can not leak untagged items. ByTag powers tag-only browsing.')"
```

---

### Task 3: CLI + web — `summarize`, `tags`, `--tag`, display

**Files:**
- Modify: `main.go` (add `summarize` + `tags` cases and functions; add `--tag` to `cmdSearch`; show summary + tags in the result line)
- Modify: `internal/web/web.go` (attach tags to results)
- Modify: `internal/web/templates/results.html` (show summary + tags)

**Interfaces:**
- Consumes: `summarize.FromEnv`/`Summarize`, `store.PendingSummary`/`SaveSummary`/`TagCounts`/`ByTag`/`AttachTags`, `store.Filter.Tag`, `search.Hybrid`.
- Produces: `glane summarize [--limit N]`, `glane tags`, `glane search … --tag X`.

- [ ] **Step 1: Add `summarize` and `tags` dispatch + functions**

In `main.go`, add to the `switch os.Args[1]`:
```go
	case "summarize":
		cmdSummarize(s, os.Args[2:])
	case "tags":
		cmdTags(s)
```
Update the top-level usage string to include `summarize` and `tags`. Add (imports: `"context"`, `"github.com/jcgay/glane/internal/summarize"`):
```go
func cmdSummarize(s *store.Store, args []string) {
	fs := flag.NewFlagSet("summarize", flag.ExitOnError)
	limit := fs.Int("limit", 100, "max items to summarize this run")
	fs.Parse(args)
	c := summarize.FromEnv()
	if c == nil {
		fatal(fmt.Errorf("set GLANE_SUMMARY_URL to generate summaries"))
	}
	items, err := s.PendingSummary(*limit)
	if err != nil {
		fatal(err)
	}
	done, failed := 0, 0
	for _, it := range items {
		res, err := c.Summarize(context.Background(), it.ArticleTitle, it.ArticleText)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "glane: summarize item %d: %v\n", it.ID, err)
			continue
		}
		if err := s.SaveSummary(it.ID, res.Summary, res.Tags); err != nil {
			fatal(err)
		}
		done++
	}
	fmt.Printf("summarized %d items (%d failed)\n", done, failed)
}

func cmdTags(s *store.Store) {
	tags, err := s.TagCounts()
	if err != nil {
		fatal(err)
	}
	for _, tc := range tags {
		fmt.Printf("%-24s %d\n", tc.Tag, tc.Count)
	}
}
```

- [ ] **Step 2: Add `--tag` to `cmdSearch`, route tag-only browse, and show summary + tags**

Modify `cmdSearch` in `main.go`. Add the flag and routing (keep `splitQueryArgs`, `--source`, `--since`, `--limit`):
```go
	tag := fs.String("tag", "", "filter by tag")
```
After building `filter` (add `Tag: *tag` to the `store.Filter{...}`), replace the search call + print loop:
```go
	var res []store.Result
	if query == "" {
		if *tag == "" {
			fatal(fmt.Errorf("usage: glane search <query> [--source X] [--tag T] [--since Y] [--limit N]"))
		}
		res, err = s.ByTag(*tag, filter)
	} else {
		res, err = search.Hybrid(s, embed.FromEnv(), query, filter)
	}
	if err != nil {
		fatal(err)
	}
	if err := s.AttachTags(res); err != nil {
		fatal(err)
	}
	for _, r := range res {
		snippet := r.Text
		if r.ArticleSummary != "" {
			snippet = r.ArticleSummary
		}
		tagStr := ""
		if len(r.Tags) > 0 {
			tagStr = "  #" + strings.Join(r.Tags, " #")
		}
		fmt.Printf("[%s/%s] %s%s\n    %s\n", r.Source, r.Kind, trunc(snippet, 160), tagStr, r.URL)
	}
	fmt.Printf("(%d results)\n", len(res))
```
(`strings` is already imported in `main.go`. Remove the now-unused old print loop.)

- [ ] **Step 3: Show summary + tags in the web UI**

In `internal/web/web.go`, in the `/search` handler, after computing `res` and before rendering, attach tags:
```go
		if err := s.AttachTags(res); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
```
In `internal/web/templates/results.html`, replace the hit body so the summary is the snippet and tags show as labels:
```html
{{range .}}
<div class="hit">
  <span class="badge">{{.Source}}/{{.Kind}}</span>
  <div>{{if .ArticleTitle}}<strong>{{.ArticleTitle}}</strong> — {{end}}{{if .ArticleSummary}}{{.ArticleSummary}}{{else}}{{.Text}}{{end}}</div>
  {{if .Tags}}<div class="tags">{{range .Tags}}<span class="tag">{{.}}</span> {{end}}</div>{{end}}
  {{if .URL}}<a href="{{.URL}}" target="_blank" rel="noreferrer">{{.URL}}</a>{{end}}
</div>
{{else}}
<p>Aucun résultat.</p>
{{end}}
```
Add two style rules to `internal/web/templates/index.html` (inside the existing `<style>`):
```css
    .tags { margin: .2rem 0; }
    .tag { font-size: .7rem; background: #eef; border-radius: .3rem; padding: .05rem .35rem; }
```

- [ ] **Step 4: Build and verify the command surface**

Run:
```bash
mise exec -- go build -o /tmp/glane . && env -u GLANE_SUMMARY_URL /tmp/glane summarize 2>&1
GLANE_DB=/tmp/glane-tags.db /tmp/glane tags; echo "tags-exit=$?"
GLANE_DB=/tmp/glane-tags.db /tmp/glane search 2>&1
```
Expected: `glane: set GLANE_SUMMARY_URL to generate summaries` (exit non-zero); `tags` prints nothing and exits 0 (empty DB); bare `search` prints the usage error.

- [ ] **Step 5: Full regression**

Run: `mise exec -- go build ./... && mise exec -- go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add main.go internal/web/
git commit -m "$(printf '%s' '🔖 Wire summarize, tags listing, and --tag filtering

Exposes the summary/tag layer: glane summarize backfills the enriched corpus
(fail-soft per item), glane tags shows the topic vocabulary with counts, and
search --tag filters (or browses, with no query). Results now lead with the
readable summary instead of raw text and show the tags, so a forgotten bookmark
is recognizable at a glance — the whole point of the feature.')"
```

---

## Follow-up (out of scope for this plan)

Tag normalization/aliasing (`k8s` → `kubernetes`) if the free-tag vocabulary drifts enough to bother — inspect it with `glane tags` first. A documented cron/launchd entry for scheduled `sync all` (+ `enrich`/`summarize`) remains a docs task.
