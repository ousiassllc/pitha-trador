package training

import (
	"math"
	"testing"
)

func TestAUC(t *testing.T) {
	for name, tc := range map[string]struct {
		scores []float64
		wins   []bool
		want   float64
	}{
		"perfect":            {[]float64{0.1, 0.2, 0.8, 0.9}, []bool{false, false, true, true}, 1},
		"inverted":           {[]float64{0.9, 0.8, 0.2, 0.1}, []bool{false, false, true, true}, 0},
		"all tied":           {[]float64{0.5, 0.5, 0.5, 0.5}, []bool{false, true, false, true}, 0.5},
		"one swap":           {[]float64{0.1, 0.6, 0.5, 0.9}, []bool{false, false, true, true}, 0.75},
		"tie between groups": {[]float64{0.2, 0.5, 0.5, 0.9}, []bool{false, false, true, true}, 0.875},
	} {
		got := auc(tc.scores, tc.wins)
		if got == nil || math.Abs(*got-tc.want) > 1e-12 {
			t.Errorf("%s: auc = %v, want %v", name, got, tc.want)
		}
	}
	if auc([]float64{0.1, 0.9}, []bool{true, true}) != nil || auc([]float64{0.1, 0.9}, []bool{false, false}) != nil || auc(nil, nil) != nil {
		t.Error("auc of a single-class set must be undefined (nil)")
	}
}
