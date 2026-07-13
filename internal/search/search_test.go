package search

import "testing"

func TestRRFRewardsAgreement(t *testing.T) {
	// id 7 is top of both lists; id 9 appears in only one.
	fused := RRF([][]int64{{7, 9, 3}, {7, 3, 1}}, 60)
	if fused[0] != 7 {
		t.Fatalf("want 7 first, got %d", fused[0])
	}
	// id 3 (in both) should outrank id 9 (in one).
	pos := map[int64]int{}
	for i, id := range fused {
		pos[id] = i
	}
	if pos[3] > pos[9] {
		t.Fatalf("expected 3 to outrank 9, got order %v", fused)
	}
}
