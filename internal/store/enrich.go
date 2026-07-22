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

// ResetEnrichment clears prior fetch results so PendingEnrichment picks every
// item up again — used by `enrich --force` to re-fetch (re-un-shorten links,
// backfill embeddings). Stored embeddings are left in place; a successful
// re-enrich overwrites them. Returns the number of items reset.
func (s *Store) ResetEnrichment() (int64, error) {
	res, err := s.db.Exec(`
		UPDATE items SET fetch_status='', link_url='', article_title='', article_text=''
		WHERE fetch_status != ''`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) SaveEnrichment(id int64, e Enrichment) error {
	_, err := s.db.Exec(`
		UPDATE items SET link_url=?, article_title=?, article_text=?,
			fetch_status=?, fetched_at=strftime('%s','now')
		WHERE id=?`, e.LinkURL, e.Title, e.Text, e.Status, id)
	return err
}
