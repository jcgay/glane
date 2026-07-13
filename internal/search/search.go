package search

import "sort"

// RRF fuses ranked id lists via Reciprocal Rank Fusion: score = Σ 1/(k+rank).
// Returns ids sorted by descending fused score.
func RRF(rankings [][]int64, k int) []int64 {
	score := map[int64]float64{}
	for _, list := range rankings {
		for rank, id := range list {
			score[id] += 1.0 / float64(k+rank+1)
		}
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool {
		if score[ids[a]] != score[ids[b]] {
			return score[ids[a]] > score[ids[b]]
		}
		return ids[a] < ids[b] // stable tie-break
	})
	return ids
}
