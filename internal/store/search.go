package store

import (
	"fmt"
	"strings"
	"time"
)

type Filter struct {
	Source string
	Since  int64
	Limit  int
	Tag    string
}

// ParseSince converts "YYYY" or "YYYY-MM-DD" (what an <input type="date">
// emits) to a Unix timestamp for Filter.Since — the start of that day/year.
// Empty input means "no date filter", not an error. The date is read in the
// local zone: both the picker and someone typing --since mean their own
// midnight, and parsing as UTC would silently drop items east of Greenwich.
func ParseSince(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t.Unix(), nil
	}
	if t, err := time.ParseInLocation("2006", v, time.Local); err == nil {
		return t.Unix(), nil
	}
	return 0, fmt.Errorf("invalid --since %q (want YYYY or YYYY-MM-DD)", v)
}

type Result struct {
	Item
	Score   float64
	Snippet string // FTS extract with matched terms bracketed by MarkStart/MarkEnd; empty for semantic-only hits

	// Displayed fields with matched terms bracketed by MarkStart/MarkEnd, from
	// FTS highlight(). Empty for semantic-only hits — use TitleMarked/BodyMarked,
	// which fall back to the raw field. See MarkStart.
	TitleHL   string
	SummaryHL string
	TextHL    string

	// Which engine(s) surfaced this hit. Both true means FTS and semantic
	// agreed (what RRF rewards). In FTS-only mode FromSemantic is always false.
	FromFTS      bool
	FromSemantic bool
}

// TitleMarked returns the article title with matched terms bracketed, or the
// plain title for semantic-only hits (and "" when there is no title).
func (r Result) TitleMarked() string {
	if r.TitleHL != "" {
		return r.TitleHL
	}
	return r.ArticleTitle
}

// BodyMarked returns the card's body text — the summary if present, else the
// raw text — with matched terms bracketed, falling back to the raw field for
// semantic-only hits. Mirrors what the card displays.
func (r Result) BodyMarked() string {
	if r.ArticleSummary != "" {
		if r.SummaryHL != "" {
			return r.SummaryHL
		}
		return r.ArticleSummary
	}
	if r.TextHL != "" {
		return r.TextHL
	}
	return r.Text
}

// MarkStart and MarkEnd bracket matched terms inside Result.Snippet. They are
// neutral control bytes (not HTML, not printable), so the store stays render-
// agnostic: the web layer maps them to <mark>, the CLI to ANSI. See Excerpt.
const (
	MarkStart = "\x02"
	MarkEnd   = "\x03"
)

// Excerpt returns the snippet to show below the summary (matched terms still
// bracketed by MarkStart/MarkEnd), or "" when there is nothing worth showing:
// no FTS match (semantic-only hit), or the matched passage is already visible
// in the summary/text the card displays.
func (r Result) Excerpt() string {
	if r.Snippet == "" {
		return ""
	}
	shown := r.Text
	if r.ArticleSummary != "" {
		shown = r.ArticleSummary
	}
	core := strings.Trim(strings.NewReplacer(MarkStart, "", MarkEnd, "").Replace(r.Snippet), "… ")
	if core != "" && (strings.Contains(shown, core) || strings.Contains(r.ArticleTitle, core)) {
		return "" // redundant with the title/body the card already shows
	}
	return r.Snippet
}

// sanitizeFTS turns an arbitrary user query into a safe FTS5 MATCH expression:
// each whitespace-separated token becomes a double-quoted string (internal
// double-quotes doubled), so special characters are treated literally and the
// tokens are ANDed. The last token also gets a trailing "*" for type-ahead
// prefix matching ("useTa" matches "useTabs"); earlier tokens stay exact.
// Empty input yields "" (caller should treat as no query).
func sanitizeFTS(query string) string {
	fields := strings.Fields(query)
	quoted := make([]string, 0, len(fields))
	for i, f := range fields {
		q := `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
		if i == len(fields)-1 {
			q += "*"
		}
		quoted = append(quoted, q)
	}
	return strings.Join(quoted, " ")
}

func (s *Store) SearchFTS(query string, f Filter) ([]Result, error) {
	match := sanitizeFTS(query)
	if match == "" {
		return nil, nil // empty/whitespace query: no results, not an error
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	stmt := `
		SELECT i.id, i.source, i.source_id, i.kind, i.author, i.text, i.url,
		       i.created_at, i.link_url, i.article_title, i.article_summary,
		       bm25(items_fts) AS score,
		       snippet(items_fts, -1, char(2), char(3), '…', 15) AS snip,
		       highlight(items_fts, 1, char(2), char(3)) AS title_hl,
		       highlight(items_fts, 3, char(2), char(3)) AS summary_hl,
		       highlight(items_fts, 0, char(2), char(3)) AS text_hl
		FROM items_fts JOIN items i ON i.id = items_fts.rowid
		WHERE items_fts MATCH ?`
	args := []any{match}
	if f.Source != "" {
		stmt += " AND i.source = ?"
		args = append(args, f.Source)
	}
	if f.Since > 0 {
		stmt += " AND i.created_at >= ?"
		args = append(args, f.Since)
	}
	if f.Tag != "" {
		stmt += " AND EXISTS (SELECT 1 FROM item_tags t WHERE t.item_id = i.id AND t.tag = ?)"
		args = append(args, f.Tag)
	}
	stmt += " ORDER BY score LIMIT ?" // bm25: lower is better
	args = append(args, f.Limit)

	rows, err := s.db.Query(stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.ID, &r.Source, &r.SourceID, &r.Kind, &r.Author,
			&r.Text, &r.URL, &r.CreatedAt, &r.LinkURL, &r.ArticleTitle,
			&r.ArticleSummary, &r.Score, &r.Snippet,
			&r.TitleHL, &r.SummaryHL, &r.TextHL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Recent lists items newest first with no text query — both the "what did I
// save while I was away" review and the `--tag X` browse, which is that same
// listing narrowed by Filter.Tag (the tag means here exactly what it means in
// SearchFTS). No MATCH is involved, so there is no score and no highlight.
//
// Filter.Since reads created_at, which is the star date for github but the
// *publication* date for twitter/mastodon/bluesky: an old article favourited
// yesterday sorts by its own age, not by when you saved it.
func (s *Store) Recent(f Filter) ([]Result, error) {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	stmt := `
		SELECT i.id, i.source, i.source_id, i.kind, i.author, i.text, i.url,
		       i.created_at, i.link_url, i.article_title, i.article_summary
		FROM items i WHERE 1=1`
	var args []any
	if f.Source != "" {
		stmt += " AND i.source = ?"
		args = append(args, f.Source)
	}
	if f.Since > 0 {
		stmt += " AND i.created_at >= ?"
		args = append(args, f.Since)
	}
	if f.Tag != "" {
		stmt += " AND EXISTS (SELECT 1 FROM item_tags t WHERE t.item_id = i.id AND t.tag = ?)"
		args = append(args, f.Tag)
	}
	stmt += " ORDER BY i.created_at DESC LIMIT ?"
	args = append(args, f.Limit)

	rows, err := s.db.Query(stmt, args...)
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
