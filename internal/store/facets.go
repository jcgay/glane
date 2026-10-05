package store

// Facets counts what a search would return per source and per tag, so a
// filter rail can show where the matches are.
//
// Each dimension ignores its own filter and applies the others: the source
// counts are what picking another source would give, under the same tag and
// date, and the same goes for tags. With a query, only full-text matches are
// counted. Semantic search ranks every item, so "how many match" has no
// answer there.
type Facets struct {
	Total    int // every source, the source filter left out
	BySource []SourceCount
	Tags     []TagCount // most frequent first, zero counts left out
}

func (s *Store) Facets(query string, f Filter) (Facets, error) {
	match := sanitizeFTS(query)
	// from returns the items rows that match the query, joined to whatever
	// the count groups on.
	from := func(base string, f Filter) (string, []any) {
		sql := " FROM " + base
		var args []any
		if match != "" {
			sql += " JOIN items_fts ON items_fts.rowid = i.id WHERE items_fts MATCH ?"
			args = append(args, match)
		} else {
			sql += " WHERE 1=1"
		}
		where, wargs := f.where()
		return sql + where, append(args, wargs...)
	}

	var out Facets
	anySource := f
	anySource.Source = ""
	sql, args := from("items i", anySource)
	rows, err := s.db.Query("SELECT i.source, COUNT(*) c"+sql+" GROUP BY i.source ORDER BY c DESC, i.source", args...)
	if err != nil {
		return Facets{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var sc SourceCount
		if err := rows.Scan(&sc.Source, &sc.Count); err != nil {
			return Facets{}, err
		}
		out.BySource = append(out.BySource, sc)
		out.Total += sc.Count
	}
	if err := rows.Err(); err != nil {
		return Facets{}, err
	}

	anyTag := f
	anyTag.Tag = ""
	sql, args = from("item_tags tg JOIN items i ON i.id = tg.item_id", anyTag)
	rows, err = s.db.Query("SELECT tg.tag, COUNT(*) c"+sql+" GROUP BY tg.tag ORDER BY c DESC, tg.tag", args...)
	if err != nil {
		return Facets{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return Facets{}, err
		}
		out.Tags = append(out.Tags, tc)
	}
	return out, rows.Err()
}
