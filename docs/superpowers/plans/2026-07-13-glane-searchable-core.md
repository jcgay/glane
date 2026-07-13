# glane Searchable Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a single Go binary `glane` that imports the static Twitter archive and searches it in full-text and semantic modes, from a CLI and a local web UI, with optional link-content enrichment.

**Architecture:** One binary, one SQLite file. `store` owns the schema (an `items` table + an `items_fts` FTS5 mirror + an `embeddings` table) and all queries. `twitter` parses the archive into `store.Item`s. `search` fuses a full-text ranking with an optional semantic ranking via Reciprocal Rank Fusion. `enrich` fetches each item's primary link and extracts the article body. `embed` calls an OpenAI-compatible embeddings endpoint. `web` serves an htmx page. `main` wires subcommands.

**Tech Stack:** Go (stdlib `net/http`, `html/template`, `flag`, `encoding/json`), `modernc.org/sqlite` (pure-Go, FTS5 built in, no cgo), `github.com/go-shiori/go-readability`, htmx (one vendored JS file).

## Global Constraints

- Module path: `github.com/jcgay/glane` (verbatim in every import).
- No cgo. SQLite via `modernc.org/sqlite` only.
- Graceful degradation: full-text search MUST work with no embeddings endpoint and no network. Semantic search and enrichment are optional layers that fail soft.
- Data lives in one SQLite file; default path `~/.local/share/glane/glane.db`, overridable with `GLANE_DB`.
- Embeddings config from env: `GLANE_EMBED_URL`, `GLANE_EMBED_MODEL`, `GLANE_EMBED_KEY` (all optional; absent → semantic disabled).
- No npm / no JS build step. htmx is a single vendored file embedded via `embed.FS`.
- Commit style: English message, leading Unicode gitmoji, body explains *why*.

---

### Task 1: Project scaffold + store schema

**Files:**
- Create: `mise.toml`
- Create: `go.mod`
- Create: `internal/store/store.go`
- Create: `internal/store/vec.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Item struct { ID int64; Source, SourceID, Kind, Author, Text, URL string; CreatedAt int64; LinkURL, ArticleTitle, ArticleText, ArticleSummary, FetchStatus string; FetchedAt int64 }`
  - `type Store struct { ... }` with unexported `db *sql.DB`
  - `func Open(path string) (*Store, error)` — opens the DB and creates the schema if absent (idempotent).
  - `func (s *Store) Close() error`
  - `func (s *Store) Upsert(items []Item) (int, error)` — upserts on `(source, source_id)`, returns rows affected. Does not touch enrichment columns on conflict.
  - `func encodeVec(v []float32) []byte` and `func decodeVec(b []byte) []float32` (in `vec.go`, little-endian float32).

- [ ] **Step 1: Pin the toolchain with mise**

Create `mise.toml` (mise is the user's toolchain manager):
```toml
[tools]
go = "1.23"
```
Run:
```bash
mise install && mise exec -- go version
```
Expected: prints `go version go1.23.x`. Run every later `go`/`mise` step through the pinned toolchain (either `mise exec --` or with mise activated in the shell).

- [ ] **Step 2: Init the module**

Run:
```bash
mkdir -p internal/store && go mod init github.com/jcgay/glane
go get modernc.org/sqlite
```

- [ ] **Step 3: Write the failing test**

`internal/store/store_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestUpsertIsIdempotent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	it := Item{Source: "twitter", SourceID: "42", Kind: "like", Text: "hello lambda"}
	if _, err := s.Upsert([]Item{it}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert([]Item{it}); err != nil { // same key again
		t.Fatal(err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 row after re-upsert, got %d", n)
	}
}

func TestVecRoundTrip(t *testing.T) {
	in := []float32{0.1, -2, 3.5}
	out := decodeVec(encodeVec(in))
	if len(out) != len(in) {
		t.Fatalf("len %d != %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("at %d: %v != %v", i, out[i], in[i])
		}
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — `undefined: Open` / build error.

- [ ] **Step 5: Write `vec.go`**

`internal/store/vec.go`:
```go
package store

import (
	"encoding/binary"
	"math"
)

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
```

- [ ] **Step 6: Write `store.go`**

`internal/store/store.go`:
```go
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type Item struct {
	ID        int64
	Source    string
	SourceID  string
	Kind      string
	Author    string
	Text      string
	URL       string
	CreatedAt int64

	LinkURL        string
	ArticleTitle   string
	ArticleText    string
	ArticleSummary string
	FetchStatus    string
	FetchedAt      int64
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS items (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL,
  source_id TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL DEFAULT 0,
  link_url TEXT NOT NULL DEFAULT '',
  article_title TEXT NOT NULL DEFAULT '',
  article_text TEXT NOT NULL DEFAULT '',
  article_summary TEXT NOT NULL DEFAULT '',
  fetch_status TEXT NOT NULL DEFAULT '',
  fetched_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(source, source_id)
);

CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
  text, article_title, article_text, article_summary, author,
  content='items', content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS items_ai AFTER INSERT ON items BEGIN
  INSERT INTO items_fts(rowid, text, article_title, article_text, article_summary, author)
  VALUES (new.id, new.text, new.article_title, new.article_text, new.article_summary, new.author);
END;
CREATE TRIGGER IF NOT EXISTS items_ad AFTER DELETE ON items BEGIN
  INSERT INTO items_fts(items_fts, rowid, text, article_title, article_text, article_summary, author)
  VALUES ('delete', old.id, old.text, old.article_title, old.article_text, old.article_summary, old.author);
END;
CREATE TRIGGER IF NOT EXISTS items_au AFTER UPDATE ON items BEGIN
  INSERT INTO items_fts(items_fts, rowid, text, article_title, article_text, article_summary, author)
  VALUES ('delete', old.id, old.text, old.article_title, old.article_text, old.article_summary, old.author);
  INSERT INTO items_fts(rowid, text, article_title, article_text, article_summary, author)
  VALUES (new.id, new.text, new.article_title, new.article_text, new.article_summary, new.author);
END;

CREATE TABLE IF NOT EXISTS embeddings (
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  model TEXT NOT NULL,
  vector BLOB NOT NULL,
  PRIMARY KEY (item_id, model)
);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Upsert(items []Item) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// ON CONFLICT keeps existing enrichment columns untouched.
	stmt, err := tx.Prepare(`
		INSERT INTO items (source, source_id, kind, author, text, url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source, source_id) DO UPDATE SET
			kind=excluded.kind, author=excluded.author,
			text=excluded.text, url=excluded.url, created_at=excluded.created_at`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for _, it := range items {
		if _, err := stmt.Exec(it.Source, it.SourceID, it.Kind, it.Author, it.Text, it.URL, it.CreatedAt); err != nil {
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/store/`
Expected: PASS (both tests).

- [ ] **Step 8: Commit**

```bash
git add mise.toml go.mod go.sum internal/store/
git commit -m "$(printf '%s' '🗄️ Add SQLite store with FTS5 mirror

Establish the single-file data layer every other component builds on:
an items table, an FTS5 shadow kept in sync by triggers so full-text
search needs no separate index step, and an embeddings table for the
optional semantic layer. Upsert is idempotent on (source, source_id) so
re-imports and re-syncs never duplicate.')"
```

---

### Task 2: Twitter archive import

**Files:**
- Create: `internal/twitter/twitter.go`
- Test: `internal/twitter/twitter_test.go`
- Create: `internal/twitter/testdata/like.js`
- Create: `internal/twitter/testdata/tweets.js`

**Interfaces:**
- Consumes: `store.Item` from Task 1.
- Produces:
  - `func ParseLikes(data []byte) ([]store.Item, error)` — one `Item{Source:"twitter", Kind:"like", SourceID: tweetId, Text: fullText, URL: expandedUrl}` per entry.
  - `func ParseTweets(data []byte) ([]store.Item, error)` — `Kind:"repost"` if text starts with `RT @`, else `"own"`; `CreatedAt` parsed from Twitter's date format; `URL` = `https://twitter.com/i/web/status/{id}`.
  - `func Import(s *store.Store, dir string) (likes, tweets int, err error)` — reads `dir/data/like.js` and `dir/data/tweets.js`, upserts both.

- [ ] **Step 1: Create fixtures**

`internal/twitter/testdata/like.js`:
```js
window.YTD.like.part0 = [
  {
    "like" : {
      "tweetId" : "1590534767501398021",
      "fullText" : "Angular v15 is near https://t.co/xyW5CPOYmt",
      "expandedUrl" : "https://twitter.com/i/web/status/1590534767501398021"
    }
  }
]
```

`internal/twitter/testdata/tweets.js`:
```js
window.YTD.tweets.part0 = [
  {
    "tweet" : {
      "id_str" : "1560128705585897477",
      "created_at" : "Wed Aug 17 21:00:00 +0000 2022",
      "full_text" : "RT @AObuchow: check this out"
    }
  }
]
```

- [ ] **Step 2: Write the failing test**

`internal/twitter/twitter_test.go`:
```go
package twitter

import (
	"os"
	"testing"
)

func TestParseLikes(t *testing.T) {
	data, _ := os.ReadFile("testdata/like.js")
	items, err := ParseLikes(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1, got %d", len(items))
	}
	got := items[0]
	if got.SourceID != "1590534767501398021" || got.Kind != "like" || got.Source != "twitter" {
		t.Fatalf("bad item: %+v", got)
	}
	if got.Text == "" {
		t.Fatal("empty text")
	}
}

func TestParseTweetsDetectsRepost(t *testing.T) {
	data, _ := os.ReadFile("testdata/tweets.js")
	items, err := ParseTweets(data)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Kind != "repost" {
		t.Fatalf("want repost, got %q", items[0].Kind)
	}
	if items[0].CreatedAt == 0 {
		t.Fatal("created_at not parsed")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/twitter/`
Expected: FAIL — `undefined: ParseLikes`.

- [ ] **Step 4: Write `twitter.go`**

`internal/twitter/twitter.go`:
```go
package twitter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jcgay/glane/internal/store"
)

// stripPrefix drops the "window.YTD.x.partN = " assignment, leaving the JSON array.
func stripPrefix(data []byte) []byte {
	if i := bytes.IndexByte(data, '='); i >= 0 {
		return bytes.TrimSpace(data[i+1:])
	}
	return data
}

func ParseLikes(data []byte) ([]store.Item, error) {
	var raw []struct {
		Like struct {
			TweetID     string `json:"tweetId"`
			FullText    string `json:"fullText"`
			ExpandedURL string `json:"expandedUrl"`
		} `json:"like"`
	}
	if err := json.Unmarshal(stripPrefix(data), &raw); err != nil {
		return nil, err
	}
	items := make([]store.Item, 0, len(raw))
	for _, r := range raw {
		items = append(items, store.Item{
			Source: "twitter", SourceID: r.Like.TweetID, Kind: "like",
			Text: r.Like.FullText, URL: r.Like.ExpandedURL,
		})
	}
	return items, nil
}

func ParseTweets(data []byte) ([]store.Item, error) {
	var raw []struct {
		Tweet struct {
			IDStr     string `json:"id_str"`
			CreatedAt string `json:"created_at"`
			FullText  string `json:"full_text"`
		} `json:"tweet"`
	}
	if err := json.Unmarshal(stripPrefix(data), &raw); err != nil {
		return nil, err
	}
	items := make([]store.Item, 0, len(raw))
	for _, r := range raw {
		kind := "own"
		if strings.HasPrefix(r.Tweet.FullText, "RT @") {
			kind = "repost"
		}
		var ts int64
		if t, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", r.Tweet.CreatedAt); err == nil {
			ts = t.Unix()
		}
		items = append(items, store.Item{
			Source: "twitter", SourceID: r.Tweet.IDStr, Kind: kind,
			Text: r.Tweet.FullText, CreatedAt: ts,
			URL: "https://twitter.com/i/web/status/" + r.Tweet.IDStr,
		})
	}
	return items, nil
}

func Import(s *store.Store, dir string) (int, int, error) {
	likeData, err := os.ReadFile(filepath.Join(dir, "data", "like.js"))
	if err != nil {
		return 0, 0, fmt.Errorf("read like.js: %w", err)
	}
	likes, err := ParseLikes(likeData)
	if err != nil {
		return 0, 0, err
	}
	if _, err := s.Upsert(likes); err != nil {
		return 0, 0, err
	}

	tweetData, err := os.ReadFile(filepath.Join(dir, "data", "tweets.js"))
	if err != nil {
		return len(likes), 0, fmt.Errorf("read tweets.js: %w", err)
	}
	tweets, err := ParseTweets(tweetData)
	if err != nil {
		return len(likes), 0, err
	}
	if _, err := s.Upsert(tweets); err != nil {
		return len(likes), 0, err
	}
	return len(likes), len(tweets), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/twitter/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/twitter/
git commit -m "$(printf '%s' '🐦 Parse and import the Twitter archive

The archive is the one static, already-in-hand source, so it is the
fastest path to a populated index and a testable search. Files are JS
assignments wrapping a JSON array; we strip the prefix and unmarshal.
Likes carry no timestamp or author in the export, so those stay zero
rather than being faked.')"
```

---

### Task 3: Full-text search + CLI

**Files:**
- Create: `internal/store/search.go`
- Create: `internal/search/search.go`
- Test: `internal/store/search_test.go`
- Test: `internal/search/search_test.go`
- Create: `main.go`

**Interfaces:**
- Consumes: `Store`, `Item` (Task 1).
- Produces:
  - `type Filter struct { Source string; Since int64; Limit int }`
  - `type Result struct { Item; Score float64 }`
  - `func (s *Store) SearchFTS(query string, f Filter) ([]Result, error)` — FTS5 MATCH ordered by `bm25`, filtered by source/since, capped by limit (default 20). Returns ordered results.
  - `func (s *Store) GetItems(ids []int64) (map[int64]Item, error)` — hydrate by id.
  - In `internal/search`: `func RRF(rankings [][]int64, k int) []int64` — fuse ranked id lists, higher fused score first.
  - `main.go` dispatches `import twitter <dir>` and `search <query> [flags]`.

- [ ] **Step 1: Write the failing store test**

`internal/store/search_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestSearchFTSMatchesAndFilters(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{
		{Source: "twitter", SourceID: "1", Kind: "like", Text: "cold start of aws lambda"},
		{Source: "twitter", SourceID: "2", Kind: "like", Text: "kubernetes networking"},
		{Source: "github", SourceID: "3", Kind: "star", Text: "lambda calculus notes"},
	})

	res, err := s.SearchFTS("lambda", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 lambda hits, got %d", len(res))
	}

	res, _ = s.SearchFTS("lambda", Filter{Source: "github"})
	if len(res) != 1 || res[0].Source != "github" {
		t.Fatalf("source filter failed: %+v", res)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run FTS`
Expected: FAIL — `undefined: SearchFTS`.

- [ ] **Step 3: Write `internal/store/search.go`**

```go
package store

import "strings"

type Filter struct {
	Source string
	Since  int64
	Limit  int
}

type Result struct {
	Item
	Score float64
}

func (s *Store) SearchFTS(query string, f Filter) ([]Result, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	sql := `
		SELECT i.id, i.source, i.source_id, i.kind, i.author, i.text, i.url,
		       i.created_at, i.link_url, i.article_title, i.article_summary,
		       bm25(items_fts) AS score
		FROM items_fts JOIN items i ON i.id = items_fts.rowid
		WHERE items_fts MATCH ?`
	args := []any{query}
	if f.Source != "" {
		sql += " AND i.source = ?"
		args = append(args, f.Source)
	}
	if f.Since > 0 {
		sql += " AND i.created_at >= ?"
		args = append(args, f.Since)
	}
	sql += " ORDER BY score LIMIT ?" // bm25: lower is better
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
			&r.ArticleSummary, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetItems(ids []int64) (map[int64]Item, error) {
	if len(ids) == 0 {
		return map[int64]Item{}, nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(`
		SELECT id, source, source_id, kind, author, text, url, created_at,
		       link_url, article_title, article_summary
		FROM items WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Source, &it.SourceID, &it.Kind, &it.Author,
			&it.Text, &it.URL, &it.CreatedAt, &it.LinkURL, &it.ArticleTitle,
			&it.ArticleSummary); err != nil {
			return nil, err
		}
		m[it.ID] = it
	}
	return m, rows.Err()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/store/ -run FTS`
Expected: PASS.

- [ ] **Step 5: Write the failing RRF test**

`internal/search/search_test.go`:
```go
package search

import "testing"

func TestRRFRewardsAgreement(t *testing.T) {
	// id 7 is top of both lists; id 9 appears in only one.
	fused := RRF([][]int64{{7, 9, 3}, {7, 3, 1}}, 60)
	if fused[0] != 7 {
		t.Fatalf("want 7 first, got %d", fused[0])
	}
	// id 3 (in both) should outrank id 9 (in one).
	pos := map[int64]int{}
	for i, id := range fused {
		pos[id] = i
	}
	if pos[3] > pos[9] {
		t.Fatalf("expected 3 to outrank 9, got order %v", fused)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/search/`
Expected: FAIL — `undefined: RRF`.

- [ ] **Step 7: Write `internal/search/search.go`**

```go
package search

import "sort"

// RRF fuses ranked id lists via Reciprocal Rank Fusion: score = Σ 1/(k+rank).
// Returns ids sorted by descending fused score.
func RRF(rankings [][]int64, k int) []int64 {
	score := map[int64]float64{}
	for _, list := range rankings {
		for rank, id := range list {
			score[id] += 1.0 / float64(k+rank+1)
		}
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		if score[ids[a]] != score[ids[b]] {
			return score[ids[a]] > score[ids[b]]
		}
		return ids[a] < ids[b] // stable tie-break
	})
	return ids
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/search/`
Expected: PASS.

- [ ] **Step 9: Write `main.go` (import + search subcommands)**

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jcgay/glane/internal/store"
	"github.com/jcgay/glane/internal/twitter"
)

func dbPath() string {
	if p := os.Getenv("GLANE_DB"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".local", "share", "glane")
	os.MkdirAll(p, 0o755)
	return filepath.Join(p, "glane.db")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: glane <import|search|serve|enrich> ...")
		os.Exit(2)
	}
	s, err := store.Open(dbPath())
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	switch os.Args[1] {
	case "import":
		cmdImport(s, os.Args[2:])
	case "search":
		cmdSearch(s, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func cmdImport(s *store.Store, args []string) {
	if len(args) < 2 || args[0] != "twitter" {
		fatal(fmt.Errorf("usage: glane import twitter <archive-dir>"))
	}
	likes, tweets, err := twitter.Import(s, args[1])
	if err != nil {
		fatal(err)
	}
	fmt.Printf("imported %d likes, %d tweets\n", likes, tweets)
}

func cmdSearch(s *store.Store, args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	source := fs.String("source", "", "filter by source")
	limit := fs.Int("limit", 20, "max results")
	fs.Parse(args)
	if fs.NArg() == 0 {
		fatal(fmt.Errorf("usage: glane search <query> [--source] [--limit]"))
	}
	res, err := s.SearchFTS(fs.Arg(0), store.Filter{Source: *source, Limit: *limit})
	if err != nil {
		fatal(err)
	}
	for _, r := range res {
		fmt.Printf("[%s/%s] %s\n    %s\n", r.Source, r.Kind, trunc(r.Text, 120), r.URL)
	}
	fmt.Printf("(%d results)\n", len(res))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "glane:", err)
	os.Exit(1)
}
```

- [ ] **Step 10: Verify the CLI end-to-end**

Run:
```bash
go build -o /tmp/glane . && GLANE_DB=/tmp/glane-e2e.db /tmp/glane import twitter ./twitter && GLANE_DB=/tmp/glane-e2e.db /tmp/glane search lambda --limit 5
```
Expected: an import count line, then result lines for "lambda", then `(N results)`.

- [ ] **Step 11: Commit**

```bash
git add internal/store/search.go internal/store/search_test.go internal/search/ main.go
git commit -m "$(printf '%s' '🔍 Add full-text search and the CLI

This is the first end-to-end slice a user can run: import then search,
with zero network and no model. bm25 ranking comes free from FTS5. RRF
lives here now (not later) because the semantic task will reuse it
verbatim to fuse the two rankings, and fusing is easier to reason about
tested in isolation.')"
```

---

### Task 4: Web UI (`serve`)

**Files:**
- Create: `internal/web/web.go`
- Create: `internal/web/templates/index.html`
- Create: `internal/web/static/htmx.min.js` (vendored htmx 1.x)
- Test: `internal/web/web_test.go`
- Modify: `main.go` (add `serve` subcommand)

**Interfaces:**
- Consumes: `Store.SearchFTS`, `store.Filter`, `store.Result`.
- Produces: `func Serve(s *store.Store, addr string) error` — serves `GET /` (page) and `GET /search?q=&source=` (htmx fragment of result rows). Assets embedded via `embed.FS`.

- [ ] **Step 1: Vendor htmx**

Run:
```bash
mkdir -p internal/web/static internal/web/templates
curl -sL https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js -o internal/web/static/htmx.min.js
test -s internal/web/static/htmx.min.js && echo OK
```
Expected: `OK`. (If offline, drop any htmx 1.x `htmx.min.js` into that path.)

- [ ] **Step 2: Write the failing test**

`internal/web/web_test.go`:
```go
package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestSearchFragmentRendersHits(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]store.Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "aws lambda cold start", URL: "http://x"}})

	req := httptest.NewRequest("GET", "/search?q=lambda", nil)
	rec := httptest.NewRecorder()
	handler(s).ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "lambda") {
		t.Fatalf("fragment missing hit: %s", rec.Body.String())
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/web/`
Expected: FAIL — `undefined: handler`.

- [ ] **Step 4: Write templates**

`internal/web/templates/index.html`:
```html
<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>glane</title>
  <script src="/static/htmx.min.js"></script>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 780px; margin: 2rem auto; padding: 0 1rem; }
    input, select { font-size: 1rem; padding: .4rem; }
    .hit { border-bottom: 1px solid #ddd; padding: .6rem 0; }
    .badge { font-size: .75rem; background: #eee; border-radius: .3rem; padding: .1rem .4rem; }
    a { color: #06c; text-decoration: none; }
  </style>
</head>
<body>
  <h1>glane</h1>
  <input type="search" name="q" placeholder="Rechercher…" autofocus
         hx-get="/search" hx-trigger="input changed delay:300ms, search"
         hx-target="#results" hx-include="[name='source']">
  <select name="source" hx-get="/search" hx-target="#results" hx-include="[name='q']">
    <option value="">toutes sources</option>
    <option value="twitter">twitter</option>
    <option value="bluesky">bluesky</option>
    <option value="mastodon">mastodon</option>
    <option value="github">github</option>
  </select>
  <div id="results"></div>
</body>
</html>
```

`internal/web/templates/results.html`:
```html
{{range .}}
<div class="hit">
  <span class="badge">{{.Source}}/{{.Kind}}</span>
  <div>{{if .ArticleTitle}}<strong>{{.ArticleTitle}}</strong> — {{end}}{{.Text}}</div>
  {{if .URL}}<a href="{{.URL}}" target="_blank" rel="noreferrer">{{.URL}}</a>{{end}}
</div>
{{else}}
<p>Aucun résultat.</p>
{{end}}
```

- [ ] **Step 5: Write `web.go`**

```go
package web

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/jcgay/glane/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

var tmpl = template.Must(template.ParseFS(assets, "templates/*.html"))

func handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(assets)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		tmpl.ExecuteTemplate(w, "index.html", nil)
	})

	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			w.Write([]byte(""))
			return
		}
		res, err := s.SearchFTS(q, store.Filter{Source: r.URL.Query().Get("source")})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tmpl.ExecuteTemplate(w, "results.html", res)
	})
	return mux
}

func Serve(s *store.Store, addr string) error {
	return http.ListenAndServe(addr, handler(s))
}
```

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./internal/web/`
Expected: PASS.

- [ ] **Step 7: Wire `serve` into `main.go`**

Add to the `switch` in `main.go`:
```go
	case "serve":
		cmdServe(s, os.Args[2:])
```
Add the function and import `"github.com/jcgay/glane/internal/web"`:
```go
func cmdServe(s *store.Store, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "listen port")
	fs.Parse(args)
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("glane serving on http://%s\n", addr)
	if err := web.Serve(s, addr); err != nil {
		fatal(err)
	}
}
```

- [ ] **Step 8: Verify serve manually**

Run:
```bash
go build -o /tmp/glane . && GLANE_DB=/tmp/glane-e2e.db /tmp/glane serve --port 8099 &
sleep 1 && curl -s "http://127.0.0.1:8099/search?q=lambda" | head && kill %1
```
Expected: HTML fragment containing a `.hit` block.

- [ ] **Step 9: Commit**

```bash
git add internal/web/ main.go
git commit -m "$(printf '%s' '🖥️ Add local web UI over full-text search

Browsing beats a terminal for skimming a wall of saved posts, so serve a
one-page htmx front over the same SearchFTS the CLI uses — search-as-you-
type with no build step and assets embedded in the binary, keeping the
single-file distribution promise.')"
```

---

### Task 5: Link enrichment (fetch + extract, no LLM)

**Files:**
- Create: `internal/enrich/enrich.go`
- Test: `internal/enrich/enrich_test.go`
- Create: `internal/store/enrich.go`
- Test: `internal/store/enrich_test.go`
- Modify: `main.go` (add `enrich` subcommand)

**Interfaces:**
- Consumes: `Store`, `Item`.
- Produces:
  - `func FirstURL(text string) string` — first `http(s)://…` token, or `""`.
  - `func Extract(body io.Reader, pageURL string) (title, text string, err error)` — via go-readability.
  - `type Enrichment struct { LinkURL, Title, Text, Status string }`
  - `func (s *Store) PendingEnrichment(limit int) ([]Item, error)` — items where `fetch_status = ''` and `url != ''`.
  - `func (s *Store) SaveEnrichment(id int64, e Enrichment) error` — writes link/article columns + `fetched_at`.
  - `func Run(s *store.Store, hc *http.Client, limit int) (done, failed int, err error)` — the enrich loop.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/go-shiori/go-readability`

- [ ] **Step 2: Write the failing enrich test**

`internal/enrich/enrich_test.go`:
```go
package enrich

import (
	"strings"
	"testing"
)

func TestFirstURL(t *testing.T) {
	if got := FirstURL("hi https://t.co/abc and more"); got != "https://t.co/abc" {
		t.Fatalf("got %q", got)
	}
	if got := FirstURL("no link here"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestExtractPullsBody(t *testing.T) {
	html := `<html><head><title>My Post</title></head><body><article>
		<p>The cold start problem in AWS Lambda is about init latency.</p>
		<p>Provisioned concurrency helps.</p></article></body></html>`
	title, text, err := Extract(strings.NewReader(html), "http://example.com/post")
	if err != nil {
		t.Fatal(err)
	}
	if title != "My Post" {
		t.Fatalf("title %q", title)
	}
	if !strings.Contains(text, "cold start") {
		t.Fatalf("body not extracted: %q", text)
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/enrich/`
Expected: FAIL — `undefined: FirstURL`.

- [ ] **Step 4: Write `enrich.go`**

```go
package enrich

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	readability "github.com/go-shiori/go-readability"
	"github.com/jcgay/glane/internal/store"
)

var urlRe = regexp.MustCompile(`https?://[^\s]+`)

func FirstURL(text string) string { return urlRe.FindString(text) }

func Extract(body io.Reader, pageURL string) (string, string, error) {
	u, _ := url.Parse(pageURL)
	art, err := readability.FromReader(body, u)
	if err != nil {
		return "", "", err
	}
	return art.Title, art.TextContent, nil
}

type Enrichment struct {
	LinkURL string
	Title   string
	Text    string
	Status  string // "ok" or "failed"
}

func Run(s *store.Store, hc *http.Client, limit int) (int, int, error) {
	items, err := s.PendingEnrichment(limit)
	if err != nil {
		return 0, 0, err
	}
	var done, failed int
	for _, it := range items {
		link := FirstURL(it.Text)
		if link == "" {
			link = it.URL
		}
		e := Enrichment{LinkURL: link, Status: "failed"}

		resp, err := hc.Get(link)
		if err == nil && resp.StatusCode == 200 {
			if title, text, xerr := Extract(resp.Body, link); xerr == nil {
				e.Title, e.Text, e.Status = title, text, "ok"
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
		if e.Status == "ok" {
			done++
		} else {
			failed++
		}
		if err := s.SaveEnrichment(it.ID, e); err != nil {
			return done, failed, err
		}
	}
	return done, failed, nil
}

// DefaultClient is a link fetcher that gives up quickly on dead links.
func DefaultClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }
```

- [ ] **Step 5: Write the failing store test**

`internal/store/enrich_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestPendingThenSaveEnrichment(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x", URL: "http://a"}})

	p, err := s.PendingEnrichment(10)
	if err != nil || len(p) != 1 {
		t.Fatalf("pending=%d err=%v", len(p), err)
	}
	if err := s.SaveEnrichment(p[0].ID, e("Title", "body about lambda", "ok")); err != nil {
		t.Fatal(err)
	}
	// Now it is no longer pending...
	p2, _ := s.PendingEnrichment(10)
	if len(p2) != 0 {
		t.Fatalf("still pending: %d", len(p2))
	}
	// ...and its article text is searchable via FTS.
	res, _ := s.SearchFTS("lambda", Filter{})
	if len(res) != 1 {
		t.Fatalf("article text not indexed, hits=%d", len(res))
	}
}

// e is a tiny helper mirroring enrich.Enrichment fields the store needs.
func e(title, text, status string) Enrichment {
	return Enrichment{Title: title, Text: text, Status: status}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/store/ -run Enrichment`
Expected: FAIL — `undefined: PendingEnrichment` / `Enrichment`.

- [ ] **Step 7: Write `internal/store/enrich.go`**

```go
package store

// Enrichment carries extracted link content. Mirrors enrich.Enrichment but
// lives here so the store package has no dependency on enrich.
type Enrichment struct {
	LinkURL string
	Title   string
	Text    string
	Status  string
}

func (s *Store) PendingEnrichment(limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, source, source_id, kind, text, url
		FROM items WHERE fetch_status = '' AND url != '' LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Source, &it.SourceID, &it.Kind, &it.Text, &it.URL); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) SaveEnrichment(id int64, e Enrichment) error {
	_, err := s.db.Exec(`
		UPDATE items SET link_url=?, article_title=?, article_text=?,
			fetch_status=?, fetched_at=strftime('%s','now')
		WHERE id=?`, e.LinkURL, e.Title, e.Text, e.Status, id)
	return err
}
```

- [ ] **Step 8: Reconcile the two Enrichment types**

`enrich.Run` builds `store.Enrichment` when saving. Update the `SaveEnrichment` call in `enrich.go` Step 4 to pass `store.Enrichment{LinkURL: e.LinkURL, Title: e.Title, Text: e.Text, Status: e.Status}`. Edit `enrich.go`:
```go
		if err := s.SaveEnrichment(it.ID, store.Enrichment{
			LinkURL: e.LinkURL, Title: e.Title, Text: e.Text, Status: e.Status,
		}); err != nil {
```

- [ ] **Step 9: Run all affected tests**

Run: `go test ./internal/store/ ./internal/enrich/`
Expected: PASS.

- [ ] **Step 10: Wire `enrich` into `main.go`**

Add to the `switch`:
```go
	case "enrich":
		cmdEnrich(s, os.Args[2:])
```
Add (import `"github.com/jcgay/glane/internal/enrich"`):
```go
func cmdEnrich(s *store.Store, args []string) {
	fs := flag.NewFlagSet("enrich", flag.ExitOnError)
	limit := fs.Int("limit", 100, "max items to fetch this run")
	fs.Parse(args)
	done, failed, err := enrich.Run(s, enrich.DefaultClient(), *limit)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("enriched %d, failed %d\n", done, failed)
}
```

- [ ] **Step 11: Commit**

```bash
git add internal/enrich/ internal/store/enrich.go internal/store/enrich_test.go main.go
git commit -m "$(printf '%s' '🌐 Enrich items with linked article text

Most saved posts are just a link; the value is the blog behind it, so
searching only the 100-char post misses what the bookmark was for.
Fetch the primary link and extract the article body into the FTS index —
no LLM needed. Dead links (common on old t.co URLs) are marked failed and
the post stays searchable by its own text. The run is resumable: only
untouched items are pending, so it can be re-run and rate-limited.')"
```

---

### Task 6: Semantic search (embeddings + cosine + RRF fusion)

**Files:**
- Create: `internal/embed/embed.go`
- Test: `internal/embed/embed_test.go`
- Create: `internal/store/vector.go`
- Test: `internal/store/vector_test.go`
- Create: `internal/search/semantic.go`
- Test: `internal/search/semantic_test.go`
- Modify: `main.go` (embed-on-enrich + hybrid search wiring)

**Interfaces:**
- Consumes: `Store`, `Item`, `search.RRF`, `store.SearchFTS`.
- Produces:
  - `type Client struct { BaseURL, Model, APIKey string; HTTP *http.Client }`
  - `func FromEnv() *Client` — reads `GLANE_EMBED_URL/MODEL/KEY`; returns `nil` if URL unset.
  - `func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error)` — POST `{BaseURL}/embeddings` (OpenAI shape).
  - `type Embedded struct { ID int64; Vec []float32 }`
  - `func (s *Store) SaveEmbedding(id int64, model string, v []float32) error`
  - `func (s *Store) AllEmbeddings(model string) ([]Embedded, error)`
  - `func Cosine(a, b []float32) float32`
  - `func SemanticIDs(query []float32, embs []store.Embedded, limit int) []int64`

- [ ] **Step 1: Write the failing embed test**

`internal/embed/embed_test.go`:
```go
package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedParsesOpenAIShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Model: "m", HTTP: srv.Client()}
	vecs, err := c.Embed(context.Background(), []string{"hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 || vecs[0][1] != 0.2 {
		t.Fatalf("bad parse: %v", vecs)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/embed/`
Expected: FAIL — `undefined: Client`.

- [ ] **Step 3: Write `embed.go`**

```go
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

func FromEnv() *Client {
	url := os.Getenv("GLANE_EMBED_URL")
	if url == "" {
		return nil // semantic disabled
	}
	return &Client{
		BaseURL: url,
		Model:   os.Getenv("GLANE_EMBED_MODEL"),
		APIKey:  os.Getenv("GLANE_EMBED_KEY"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": c.Model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embeddings: status %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/embed/`
Expected: PASS.

- [ ] **Step 5: Write the failing store vector test**

`internal/store/vector_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadEmbeddings(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	s.Upsert([]Item{{Source: "twitter", SourceID: "1", Kind: "like", Text: "x"}})
	all, _ := s.AllEmbeddings("m")
	if len(all) != 0 {
		t.Fatal("expected none yet")
	}
	if err := s.SaveEmbedding(1, "m", []float32{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	all, _ = s.AllEmbeddings("m")
	if len(all) != 1 || all[0].Vec[2] != 3 {
		t.Fatalf("bad load: %+v", all)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/store/ -run Embeddings`
Expected: FAIL — `undefined: AllEmbeddings`.

- [ ] **Step 7: Write `internal/store/vector.go`**

```go
package store

type Embedded struct {
	ID  int64
	Vec []float32
}

func (s *Store) SaveEmbedding(id int64, model string, v []float32) error {
	_, err := s.db.Exec(`
		INSERT INTO embeddings (item_id, model, vector) VALUES (?, ?, ?)
		ON CONFLICT(item_id, model) DO UPDATE SET vector=excluded.vector`,
		id, model, encodeVec(v))
	return err
}

func (s *Store) AllEmbeddings(model string) ([]Embedded, error) {
	rows, err := s.db.Query(`SELECT item_id, vector FROM embeddings WHERE model = ?`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Embedded
	for rows.Next() {
		var id int64
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		out = append(out, Embedded{ID: id, Vec: decodeVec(b)})
	}
	return out, rows.Err()
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/store/ -run Embeddings`
Expected: PASS.

- [ ] **Step 9: Write the failing semantic test**

`internal/search/semantic_test.go`:
```go
package search

import (
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestSemanticIDsRanksByCosine(t *testing.T) {
	q := []float32{1, 0}
	embs := []store.Embedded{
		{ID: 1, Vec: []float32{0, 1}},   // orthogonal, far
		{ID: 2, Vec: []float32{1, 0.1}}, // near
	}
	ids := SemanticIDs(q, embs, 10)
	if ids[0] != 2 {
		t.Fatalf("want 2 first, got %v", ids)
	}
}
```

- [ ] **Step 10: Run to verify it fails**

Run: `go test ./internal/search/ -run Semantic`
Expected: FAIL — `undefined: SemanticIDs`.

- [ ] **Step 11: Write `internal/search/semantic.go`**

```go
package search

import (
	"math"
	"sort"

	"github.com/jcgay/glane/internal/store"
)

func Cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// SemanticIDs returns item ids ordered by descending cosine to the query.
func SemanticIDs(query []float32, embs []store.Embedded, limit int) []int64 {
	type scored struct {
		id int64
		s  float32
	}
	ranked := make([]scored, len(embs))
	for i, e := range embs {
		ranked[i] = scored{e.ID, Cosine(query, e.Vec)}
	}
	sort.Slice(ranked, func(a, b int) bool { return ranked[a].s > ranked[b].s })
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	ids := make([]int64, len(ranked))
	for i, r := range ranked {
		ids[i] = r.id
	}
	return ids
}
```

- [ ] **Step 12: Run to verify it passes**

Run: `go test ./internal/search/ -run Semantic`
Expected: PASS.

- [ ] **Step 13: Wire hybrid search + embed-on-enrich into `main.go`**

Replace `cmdSearch` body so it fuses FTS with semantic when embeddings are configured. The text embedded per item is `article_summary` if present, else `article_title + text` (kept short). Add helper `embedText(it store.Item) string`. Add imports `"context"`, `"github.com/jcgay/glane/internal/embed"`.

```go
func embedText(it store.Item) string {
	if it.ArticleSummary != "" {
		return it.ArticleSummary
	}
	t := it.ArticleTitle + " " + it.Text
	if len(t) > 2000 {
		t = t[:2000]
	}
	return t
}

func cmdSearch(s *store.Store, args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	source := fs.String("source", "", "filter by source")
	limit := fs.Int("limit", 20, "max results")
	fs.Parse(args)
	if fs.NArg() == 0 {
		fatal(fmt.Errorf("usage: glane search <query> [--source] [--limit]"))
	}
	q := fs.Arg(0)
	filter := store.Filter{Source: *source, Limit: *limit}

	ftsRes, err := s.SearchFTS(q, filter)
	if err != nil {
		fatal(err)
	}

	results := ftsRes
	// Optional semantic layer, fused via RRF when an embeddings endpoint exists.
	if c := embed.FromEnv(); c != nil {
		if sem := semanticResults(s, c, q, filter); sem != nil {
			results = fuse(s, ftsRes, sem, *limit)
		}
	}
	for _, r := range results {
		fmt.Printf("[%s/%s] %s\n    %s\n", r.Source, r.Kind, trunc(r.Text, 120), r.URL)
	}
	fmt.Printf("(%d results)\n", len(results))
}

func semanticResults(s *store.Store, c *embed.Client, q string, f store.Filter) []int64 {
	qv, err := c.Embed(context.Background(), []string{q})
	if err != nil || len(qv) == 0 {
		return nil // fail soft to FTS only
	}
	embs, err := s.AllEmbeddings(c.Model)
	if err != nil || len(embs) == 0 {
		return nil
	}
	return search.SemanticIDs(qv[0], embs, 100)
}

func fuse(s *store.Store, fts []store.Result, semIDs []int64, limit int) []store.Result {
	ftsIDs := make([]int64, len(fts))
	for i, r := range fts {
		ftsIDs[i] = r.ID
	}
	fused := search.RRF([][]int64{ftsIDs, semIDs}, 60)
	if len(fused) > limit {
		fused = fused[:limit]
	}
	items, _ := s.GetItems(fused)
	out := make([]store.Result, 0, len(fused))
	for _, id := range fused {
		if it, ok := items[id]; ok {
			out = append(out, store.Result{Item: it})
		}
	}
	return out
}
```
Add import `"github.com/jcgay/glane/internal/search"`.

- [ ] **Step 14: Embed during enrichment**

In `internal/enrich/enrich.go`, extend `Run` to accept an optional embedder and store a vector when extraction succeeds. Change signature to `func Run(s *store.Store, hc *http.Client, emb *embed.Client, limit int) (int, int, error)` and after a successful `SaveEnrichment`, if `emb != nil`, embed `title + "\n" + text` (truncated to 2000 chars) and `SaveEmbedding`. Fail soft — an embedding error must not fail the enrich run. Update `cmdEnrich` to pass `embed.FromEnv()`.

```go
		if e.Status == "ok" && emb != nil {
			text := e.Title + "\n" + e.Text
			if len(text) > 2000 {
				text = text[:2000]
			}
			if vecs, verr := emb.Embed(context.Background(), []string{text}); verr == nil && len(vecs) > 0 {
				_ = s.SaveEmbedding(it.ID, emb.Model, vecs[0])
			}
		}
```
(Add imports `"context"` and `"github.com/jcgay/glane/internal/embed"` to `enrich.go`.)

- [ ] **Step 15: Full build + regression run**

Run: `go build ./... && go test ./...`
Expected: build succeeds, all tests PASS.

- [ ] **Step 16: Verify graceful degradation manually**

Run (no embeddings env → FTS only, must still work):
```bash
unset GLANE_EMBED_URL
GLANE_DB=/tmp/glane-e2e.db /tmp/glane search lambda
```
Expected: results print normally, no errors.

- [ ] **Step 17: Commit**

```bash
git add internal/embed/ internal/store/vector.go internal/store/vector_test.go internal/search/semantic.go internal/search/semantic_test.go internal/enrich/enrich.go main.go
git commit -m "$(printf '%s' '🧠 Add optional semantic search fused with full-text

Semantic recall is what lets you find a post by meaning when you have
forgotten its words — the whole point of the tool. It is a layer, not a
requirement: the embeddings endpoint is a configurable OpenAI-compatible
URL (local Ollama or remote), and when it is absent or unreachable search
silently falls back to full-text. When both are available we fuse them
with RRF so a hit strong on either signal surfaces. Brute-force cosine is
plenty fast at personal scale.')"
```

---

## Follow-up (out of scope for this plan)

A second plan will add the live connectors (`sync github`, `sync mastodon`, `sync bluesky` with per-source cursors) and the optional LLM summary step (`article_summary`), plus a documented cron/launchd entry for `sync all`. The schema, enrichment loop, and search already accommodate them (the `source`/`kind` columns, `article_summary` field, and embedding-on-enrich hook are in place).
