package search

import (
	"testing"

	"github.com/jcgay/glane/internal/store"
)

func TestSemanticIDsRanksByCosine(t *testing.T) {
	q := []float32{1, 0}
	embs := []store.Embedded{
		{ID: 1, Vec: []float32{0, 1}},   // orthogonal, far
		{ID: 2, Vec: []float32{1, 0.1}}, // near
	}
	ids := SemanticIDs(q, embs, 10)
	if ids[0] != 2 {
		t.Fatalf("want 2 first, got %v", ids)
	}
}
