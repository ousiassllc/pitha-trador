package experiment

import (
	"errors"
	"math"
	"testing"
)

func near(got *float64, want, tol float64) bool {
	return got != nil && math.Abs(*got-want) <= tol
}

// R: t.test(extra ~ group, data = sleep) (Welch is R's default) gives
//
//	t = -1.8608, df = 17.776, p-value = 0.07939
//	95 percent confidence interval: -3.3654832  0.2054832
//	mean in group 1 0.75, mean in group 2 2.33
//
// group 1 is "before" and group 2 "after" here, so the difference of the means
// (after - before) is +1.58 and the interval and t flip sign.
func TestWelchMatchesR(t *testing.T) {
	g1 := []float64{0.7, -1.6, -0.2, -1.2, -0.1, 3.4, 3.7, 0.8, 0.0, 2.0}
	g2 := []float64{1.9, 0.8, 1.1, 0.1, -0.1, 4.4, 5.5, 1.6, 4.6, 3.4}
	got := Welch(g1, g2)
	if got.CILevel != 0.95 {
		t.Errorf("ci_level = %v, want 0.95", got.CILevel)
	}
	checks := []struct {
		name string
		got  *float64
		want float64
		tol  float64
	}{
		{"before mean", got.Before.MeanJPY, 0.75, 1e-12},
		{"after mean", got.After.MeanJPY, 2.33, 1e-12},
		{"before sd", got.Before.SDJPY, 1.789010, 1e-6}, // R: sd(g1)
		{"after sd", got.After.SDJPY, 2.002249, 1e-6},   // R: sd(g2)
		{"diff", got.Diff, 1.58, 1e-12},
		{"ci_low", got.CILow, -0.2054832, 1e-6},
		{"ci_high", got.CIHigh, 3.3654832, 1e-6},
		{"t", got.T, 1.8608, 1e-4},
		{"df", got.DF, 17.776, 1e-3},
	}
	for _, c := range checks {
		if !near(c.got, c.want, c.tol) {
			t.Errorf("%s = %v, want %v ± %v", c.name, ptr(c.got), c.want, c.tol)
		}
	}
	if got.Before.N != 10 || got.After.N != 10 || !got.Computable() {
		t.Errorf("n = %d/%d computable=%v", got.Before.N, got.After.N, got.Computable())
	}
}

// Welch (not pooled): with very different variances and sizes the degrees of
// freedom are the Welch–Satterthwaite ones, not n1+n2-2. Reference from the
// formulas (hand computation, not an R printout)
//
//	se² = s1²/n1 + s2²/n2, df = se⁴ / ((s1²/n1)²/(n1-1) + (s2²/n2)²/(n2-1))
//
// for the means 3 and 9, s² 2.5 and 24: se² = 0.5 + 3, df = 12.25 / (0.0625 +
// 9/7) = 9.0861 (a pooled test would use 11).
func TestWelchUnequalVariances(t *testing.T) {
	got := Welch([]float64{1, 2, 3, 4, 5}, []float64{2, 4, 6, 8, 10, 12, 14, 16})
	if !near(got.DF, 9.086092715, 1e-8) || !near(got.T, 6/math.Sqrt(3.5), 1e-9) {
		t.Errorf("df = %v t = %v, want 9.0861 and %v", ptr(got.DF), ptr(got.T), 6/math.Sqrt(3.5))
	}
}

// Degenerate input never panics and gives no NaN / infinity: what cannot be
// computed is nil and the verdict is withheld.
func TestWelchDegenerate(t *testing.T) {
	tests := []struct {
		name          string
		before, after []float64
		wantN         [2]int
	}{
		{"both empty", nil, nil, [2]int{0, 0}},
		{"one trade each", []float64{1}, []float64{2}, [2]int{1, 1}},
		{"n<2 on one side", []float64{1, 2, 3}, []float64{5}, [2]int{3, 1}},
		{"all identical on both sides", []float64{4, 4, 4}, []float64{9, 9, 9}, [2]int{3, 3}},
		{"identical across the two", []float64{4, 4, 4}, []float64{4, 4, 4}, [2]int{3, 3}},
		{"infinite value", []float64{1, 2, math.Inf(1)}, []float64{1, 2, 3}, [2]int{3, 3}},
		{"nan value", []float64{1, 2, math.NaN()}, []float64{1, 2, 3}, [2]int{3, 3}},
	}
	for _, tt := range tests {
		got := Welch(tt.before, tt.after)
		if got.Computable() || got.T != nil || got.DF != nil {
			t.Errorf("%s: computed %+v, want nothing computable", tt.name, got)
		}
		if got.Before.N != tt.wantN[0] || got.After.N != tt.wantN[1] {
			t.Errorf("%s: n = %d/%d, want %v", tt.name, got.Before.N, got.After.N, tt.wantN)
		}
	}
	// One side constant is fine as long as the other has variance.
	if got := Welch([]float64{4, 4, 4}, []float64{1, 5, 9}); !got.Computable() {
		t.Errorf("one constant side: not computable, want an interval: %+v", got)
	}
}

func TestCheckMinTrades(t *testing.T) {
	for _, ok := range []int{MinTradesFloor, MinTradesFloor + 1, MaxMinTrades} {
		if err := CheckMinTrades(ok); err != nil {
			t.Errorf("CheckMinTrades(%d) = %v, want nil", ok, err)
		}
	}
	for _, low := range []int{MinTradesFloor - 1, 1, 0, -1} {
		if err := CheckMinTrades(low); !errors.Is(err, ErrMinTradesBelowFloor) {
			t.Errorf("CheckMinTrades(%d) = %v, want ErrMinTradesBelowFloor", low, err)
		}
	}
	if err := CheckMinTrades(MaxMinTrades + 1); err == nil || errors.Is(err, ErrMinTradesBelowFloor) {
		t.Errorf("CheckMinTrades(max+1) = %v, want a too-large error", err)
	}
}
