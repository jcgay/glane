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

// sanitizeFTS turns an arbitrary user query into a safe FTS5 MATCH expression:
// each whitespace-separated token becomes a double-quoted string (internal
// double-quotes doubled), so special characters are treated literally and the
// tokens are ANDed. Empty input yields "" (caller should treat as no query).
func sanitizeFTS(query string) string {
	fields := strings.Fields(query)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
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
	args := []any{sanitizeFTS(query)}
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
