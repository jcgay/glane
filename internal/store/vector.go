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
