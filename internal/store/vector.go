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

// EmbeddingModels returns the distinct model names present in the embeddings
// table. Lets the search layer tell "nothing enriched yet" apart from "a changed
// GLANE_EMBED_MODEL orphaned every stored vector" and warn accordingly.
func (s *Store) EmbeddingModels() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT model FROM embeddings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AllEmbeddings(model string, f Filter) ([]Embedded, error) {
	sql := `SELECT e.item_id, e.vector FROM embeddings e
		JOIN items i ON i.id = e.item_id
		WHERE e.model = ?`
	args := []any{model}
	if f.Source != "" {
		sql += " AND i.source = ?"
		args = append(args, f.Source)
	}
	if f.Since > 0 {
		sql += " AND i.created_at >= ?"
		args = append(args, f.Since)
	}
	if f.Tag != "" {
		sql += " AND EXISTS (SELECT 1 FROM item_tags t WHERE t.item_id = i.id AND t.tag = ?)"
		args = append(args, f.Tag)
	}
	rows, err := s.db.Query(sql, args...)
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
