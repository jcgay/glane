package search

import (
	"context"

	"github.com/jcgay/glane/internal/embed"
	"github.com/jcgay/glane/internal/store"
)

// Hybrid runs full-text search and, when an embeddings client is configured,
// fuses it with semantic ranking via RRF. It always falls back to full-text
// results (never an error from the semantic layer): a nil client, an embed
// call failure, or no stored vectors all degrade silently to FTS-only.
func Hybrid(s *store.Store, c *embed.Client, query string, f store.Filter) ([]store.Result, error) {
	fts, err := s.SearchFTS(query, f)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return fts, nil
	}
	qv, err := c.Embed(context.Background(), []string{query})
	if err != nil || len(qv) == 0 {
		return fts, nil
	}
	embs, err := s.AllEmbeddings(c.Model, f)
	if err != nil || len(embs) == 0 {
		return fts, nil
	}
	semIDs := SemanticIDs(qv[0], embs, 100)
	ftsIDs := make([]int64, len(fts))
	for i, r := range fts {
		ftsIDs[i] = r.ID
	}
	fused := RRF([][]int64{ftsIDs, semIDs}, 60)
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if len(fused) > limit {
		fused = fused[:limit]
	}
	items, err := s.GetItems(fused)
	if err != nil {
		return fts, nil // DB error: fall back to full-text
	}
	out := make([]store.Result, 0, len(fused))
	for _, id := range fused {
		if it, ok := items[id]; ok {
			out = append(out, store.Result{Item: it})
		}
	}
	return out, nil
}
