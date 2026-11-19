package stats_test

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// Holm's textbook example: sorted p = .01 .04 .03 .005 → adjusted .03 .06 .06 .02 (m = 4).
func TestHolm(t *testing.T) {
	got := stats.Holm([]float64{0.01, 0.04, 0.03, 0.005})
	want := []float64{0.03, 0.06, 0.06, 0.02}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Errorf("Holm[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if got := stats.Holm([]float64{0.9, 0.8}); got[0] != 1 || got[1] != 1 {
		t.Errorf("adjusted p must be capped at 1 and monotone: %v", got)
	}
	if len(stats.Holm(nil)) != 0 {
		t.Error("Holm of nothing must be empty")
	}
}

func TestPValue(t *testing.T) {
	p, ok := stats.PValue(1.959964, 1)
	if !ok || math.Abs(p-0.05) > 1e-5 {
		t.Errorf("PValue(1.96, 1) = %v, %v; want 0.05", p, ok)
	}
	if _, ok := stats.PValue(1, 0); ok {
		t.Error("a zero standard error must be undefined")
	}
	if _, ok := stats.PValue(math.NaN(), 1); ok {
		t.Error("NaN mean must be undefined")
	}
}
