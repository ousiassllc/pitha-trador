package labelcompare

import (
	"math/rand/v2"
	"testing"
)

// A reproducible measurement of what the Newey-West correction does at the
// day counts this package judges on (docs/architecture/paper-results-mcp-experiments-labels.md §2.2):
// two windows of n days whose day means are AR(1) with coefficient rho and NO
// true difference, 20,000 draws from a fixed seed, and how often the 95%
// interval of the difference excludes 0 (the nominal rate is 0.05).
//
// The correction narrows the gap but does not close it at 8 days (it estimates
// an autocovariance from few pairs), and with independent days it costs a little
// (0.072 at n = 8). The test pins that it is not worse than the plain day
// interval under positive correlation and not out of hand without it, so the
// numbers in the docs stay true.
func TestNeweyWestFalsePositiveRate(t *testing.T) {
	const reps = 20000
	for _, rho := range []float64{0, 0.3, 0.5} {
		for _, n := range []int{8, 12, 20} {
			rng := rand.New(rand.NewPCG(1, 2))
			window := func() []float64 {
				x := make([]float64, n)
				prev := rng.NormFloat64()
				for i := range x {
					prev = rho*prev + rng.NormFloat64()
					x[i] = prev
				}
				return x
			}
			var plain, corrected int
			for range reps {
				b, a := window(), window()
				if excludesZero(diffOf(b, a, 0)) {
					plain++
				}
				if excludesZero(diffOf(b, a, NeweyWestLagDays)) {
					corrected++
				}
			}
			p, c := float64(plain)/reps, float64(corrected)/reps
			t.Logf("rho=%.1f n=%2d: false-positive rate plain day interval %.3f, Newey-West %.3f", rho, n, p, c)
			if rho >= 0.5 && c >= p {
				t.Errorf("rho=%.1f n=%d: corrected %.3f must be below plain %.3f", rho, n, c, p)
			}
			if rho == 0 && c > 0.08 {
				t.Errorf("rho=0 n=%d: corrected %.3f, the cost of the correction with independent days must stay under 0.08", n, c)
			}
		}
	}
}

func excludesZero(d *Diff) bool {
	return d != nil && d.CILow != nil && d.CIHigh != nil && (*d.CILow > 0 || *d.CIHigh < 0)
}
