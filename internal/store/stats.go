package store

// SourceCount is the number of items for one source.
type SourceCount struct {
	Source string
	Count  int
}

// SourceSync is the last sync_state.updated_at recorded for one source. Only
// sources that have actually run a live sync appear here — a one-shot import
// source (twitter) never has a sync_state row.
type SourceSync struct {
	Source    string
	UpdatedAt int64 // unix seconds
}

// Stats is a point-in-time snapshot of what's indexed, computed on read.
type Stats struct {
	Total            int
	BySource         []SourceCount
	Enriched         int // items with fetch_status = 'ok'
	Summarized       int // items with article_summary != ''
	Embedded         int // distinct item_id in embeddings
	DistinctTags     int // distinct tag in item_tags
	LastSyncBySource []SourceSync
}

// Stats aggregates counts across items, embeddings, item_tags and sync_state.
// It returns a real error like other store methods (SearchFTS, TagCounts) —
// callers decide how to degrade (CLI: fatal; web: log + zero-value render).
func (s *Store) Stats() (Stats, error) {
	var st Stats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&st.Total); err != nil {
		return Stats{}, err
	}

	srcRows, err := s.db.Query(`SELECT source, COUNT(*) c FROM items GROUP BY source ORDER BY c DESC, source`)
	if err != nil {
		return Stats{}, err
	}
	for srcRows.Next() {
		var sc SourceCount
		if err := srcRows.Scan(&sc.Source, &sc.Count); err != nil {
			srcRows.Close()
			return Stats{}, err
		}
		st.BySource = append(st.BySource, sc)
	}
	if err := srcRows.Err(); err != nil {
		srcRows.Close()
		return Stats{}, err
	}
	srcRows.Close()

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE fetch_status = 'ok'`).Scan(&st.Enriched); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE article_summary != ''`).Scan(&st.Summarized); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT item_id) FROM embeddings`).Scan(&st.Embedded); err != nil {
		return Stats{}, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT tag) FROM item_tags`).Scan(&st.DistinctTags); err != nil {
		return Stats{}, err
	}

	syncRows, err := s.db.Query(`SELECT source, updated_at FROM sync_state ORDER BY source`)
	if err != nil {
		return Stats{}, err
	}
	defer syncRows.Close()
	for syncRows.Next() {
		var ss SourceSync
		if err := syncRows.Scan(&ss.Source, &ss.UpdatedAt); err != nil {
			return Stats{}, err
		}
		st.LastSyncBySource = append(st.LastSyncBySource, ss)
	}
	return st, syncRows.Err()
}
