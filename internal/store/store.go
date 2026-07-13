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
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
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
