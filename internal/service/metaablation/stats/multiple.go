package stats

import (
	"math"
	"slices"
)

// PValue is the two-sided p-value of "the true mean is 0" under a normal
// approximation of the estimate: 2 * (1 - Phi(|mean| / se)). It is the
// descriptive number the multiple-comparison correction (Holm) is applied to,
// not a verdict. ok is false when se is not positive (fewer than two days, or no
// variation): nothing is said then.
func PValue(mean, se float64) (p float64, ok bool) {
	if !(se > 0) || math.IsNaN(mean) || math.IsInf(mean, 0) { // !(se > 0) also catches NaN
		return 0, false
	}
	return math.Erfc(math.Abs(mean) / se / math.Sqrt2), true
}

// Holm returns the Holm-Bonferroni step-down adjusted p-values of p, in the
// order of p: the smallest p is multiplied by m, the next by m-1, and so on, each
// adjusted value is at least the one before it (monotone) and at most 1. It
// controls the family-wise error rate for any dependence between the tests. An
// empty input returns an empty slice.
func Holm(p []float64) []float64 {
	m := len(p)
	out := make([]float64, m)
	order := make([]int, m)
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case p[a] < p[b]:
			return -1
		case p[a] > p[b]:
			return 1
		}
		return 0
	})
	running := 0.0
	for rank, idx := range order {
		adj := min(1, float64(m-rank)*p[idx])
		running = max(running, adj)
		out[idx] = running
	}
	return out
}
