package search

import (
	"math"
	"sort"

	"github.com/jcgay/glane/internal/store"
)

func Cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// SemanticIDs returns item ids ordered by descending cosine to the query.
func SemanticIDs(query []float32, embs []store.Embedded, limit int) []int64 {
	type scored struct {
		id int64
		s  float32
	}
	ranked := make([]scored, len(embs))
	for i, e := range embs {
		ranked[i] = scored{e.ID, Cosine(query, e.Vec)}
	}
	sort.Slice(ranked, func(a, b int) bool { return ranked[a].s > ranked[b].s })
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	ids := make([]int64, len(ranked))
	for i, r := range ranked {
		ids[i] = r.id
	}
	return ids
}
