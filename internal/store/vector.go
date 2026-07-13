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
