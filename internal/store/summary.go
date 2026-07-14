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
