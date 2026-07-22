package search

import (
	"context"
	"fmt"
	"os"

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
		if err == nil {
			warnModelMismatch(s, c.Model)
		}
		return fts, nil
	}
	semIDs := SemanticIDs(qv[0], embs, 100)
	ftsIDs := make([]int64, len(fts))
	snips := make(map[int64]string, len(fts))
	for i, r := range fts {
		ftsIDs[i] = r.ID
		snips[r.ID] = r.Snippet // keep FTS snippets before RRF re-fetches bare items
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
			out = append(out, store.Result{Item: it, Snippet: snips[id]}) // "" for semantic-only hits
		}
	}
	return out, nil
}

// warnModelMismatch prints a one-line stderr warning when the embeddings table
// holds vectors, but none for the configured model — the signature of a changed
// GLANE_EMBED_MODEL that silently orphaned every stored vector. Search still
// succeeds (degraded to FTS-only, no error surfaced); the warning just tells the
// operator to re-run `glane enrich` instead of debugging empty semantic results.
func warnModelMismatch(s *store.Store, model string) {
	models, err := s.EmbeddingModels()
	if err != nil || !staleModel(models, model) {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: no embeddings for model %q; stored vectors use %v — run `glane enrich` to re-embed (searching full-text only)\n", model, models)
}

// staleModel reports whether embeddings exist but none for the wanted model, i.e.
// the model was switched. An empty table (nothing enriched) or a table that
// contains the wanted model (empty result came from the filter) is not stale.
func staleModel(models []string, want string) bool {
	if len(models) == 0 {
		return false
	}
	for _, m := range models {
		if m == want {
			return false
		}
	}
	return true
}
