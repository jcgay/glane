package store

import "database/sql"

func (s *Store) GetCursor(source string) (string, error) {
	var cursor string
	err := s.db.QueryRow(`SELECT cursor FROM sync_state WHERE source = ?`, source).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return cursor, nil
}

func (s *Store) SetCursor(source, cursor string) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_state (source, cursor, updated_at)
		VALUES (?, ?, strftime('%s','now'))
		ON CONFLICT(source) DO UPDATE SET
			cursor=excluded.cursor, updated_at=excluded.updated_at`,
		source, cursor)
	return err
}
