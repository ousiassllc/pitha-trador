package actuals

import (
	"math"
	"testing"
)

func seq(n int, f func(i int) float64) (v []float64, days []int64) {
	for i := range n {
		v = append(v, f(i))
		days = append(days, int64(i))
	}
	return v, days
}

// Cluster-robust interval of [1 2 3 4] on two days: deviations sum to -2 and 2
// per day, SE = sqrt(2/1 * 8) / 4 = 1, half width = t(1) = 12.706.
func TestMeanMetric_ClusterRobustInterval(t *testing.T) {
	m := meanMetric([]float64{1, 2, 3, 4}, []int64{1, 1, 2, 2}, DefaultCriteria())
	if !m.Defined || !m.HasCI || m.N != 4 || m.Value != 2.5 {
		t.Fatalf("metric = %+v", m)
	}
	if math.Abs(m.Lo-(2.5-12.706)) > 1e-9 || math.Abs(m.Hi-(2.5+12.706)) > 1e-9 {
		t.Errorf("CI = [%v, %v], want 2.5 ± 12.706", m.Lo, m.Hi)
	}
}

// Correlation within a day widens the interval: the same values spread over
// distinct days are far tighter than when each day's trades move together.
func TestMeanMetric_CorrelatedDaysAreWider(t *testing.T) {
	c := DefaultCriteria()
	vals := []float64{5, 5, 5, 5, -5, -5, -5, -5, 5, 5, 5, 5, -5, -5, -5, -5}
	independent := make([]int64, len(vals))
	clustered := make([]int64, len(vals))
	for i := range vals {
		independent[i] = int64(i)
		clustered[i] = int64(i / 4)
	}
	a, b := meanMetric(vals, independent, c), meanMetric(vals, clustered, c)
	if !(b.Hi-b.Lo > a.Hi-a.Lo) {
		t.Errorf("clustered width %v must exceed independent width %v", b.Hi-b.Lo, a.Hi-a.Lo)
	}
}

// Boundary: a metric is reliable from exactly MinSample observations over at
// least MinClusters days, and not one observation or day below.
func TestMeanMetric_MinSampleBoundary(t *testing.T) {
	c := Criteria{MinSample: 30, MinClusters: 5}
	noisy := func(i int) float64 { return float64(i % 7) }
	for _, tc := range []struct {
		name         string
		n, clusters  int
		wantReliable bool
	}{
		{"29 observations", 29, 29, false},
		{"30 observations", 30, 30, true},
		{"31 observations", 31, 31, true},
		{"30 observations over 4 days", 30, 4, false},
		{"30 observations over 5 days", 30, 5, true},
	} {
		v, days := seq(tc.n, noisy)
		for i := range days {
			days[i] = int64(i % tc.clusters)
		}
		// meanMetric sums deviations per day, so day order does not matter.
		if m := meanMetric(v, days, c); m.Reliable != tc.wantReliable || m.N != tc.n {
			t.Errorf("%s: reliable = %v (n=%d), want %v", tc.name, m.Reliable, m.N, tc.wantReliable)
		}
	}
}

func TestMeanMetric_SingleDayHasNoInterval(t *testing.T) {
	v, _ := seq(40, func(i int) float64 { return float64(i) })
	m := meanMetric(v, make([]int64, 40), Criteria{MinSample: 30, MinClusters: 1})
	if m.HasCI || m.Reliable || !m.Defined {
		t.Errorf("one day = %+v, want a value without interval, 判定不能", m)
	}
}

func TestMeanMetric_Empty(t *testing.T) {
	if m := meanMetric(nil, nil, DefaultCriteria()); m.Defined || m.Reliable || m.N != 0 {
		t.Errorf("empty = %+v", m)
	}
}

func TestProportionMetric_ClampedToUnitInterval(t *testing.T) {
	v := []float64{1, 1, 1, 1, 1, 1, 0}
	days := []int64{1, 2, 3, 4, 5, 6, 7}
	m := proportionMetric(v, days, DefaultCriteria())
	if m.Lo < 0 || m.Hi > 1 {
		t.Errorf("CI = [%v, %v], want within [0, 1]", m.Lo, m.Hi)
	}
}

func TestTCritical95(t *testing.T) {
	for df, want := range map[int]float64{1: 12.706, 4: 2.776, 30: 2.042, 31: 2.021, 50: 2.000, 100: 1.980, 1000: 1.960} {
		if got := tCritical95(df); got != want {
			t.Errorf("t(%d) = %v, want %v", df, got, want)
		}
	}
}

// The bootstrap interval is reproducible and brackets the point estimate; the
// drawdown is path dependent so whole days are redrawn.
func TestBootstrapMetric_ReproducibleAndBracketing(t *testing.T) {
	c := Criteria{MinSample: 30, MinClusters: 5}
	pnl, days := seq(60, func(i int) float64 {
		if i%3 == 0 {
			return -80
		}
		return 50
	})
	for i := range days {
		days[i] = int64(i / 3)
	}
	a := bootstrapMetric(pnl, days, profitFactor, c)
	b := bootstrapMetric(pnl, days, profitFactor, c)
	if a != b {
		t.Fatalf("bootstrap not reproducible: %+v vs %+v", a, b)
	}
	// Every day is identical: profit 100 / loss 80 whatever the resample.
	if !a.Reliable || a.Value != 1.25 || a.Lo != 1.25 || a.Hi != 1.25 {
		t.Errorf("PF = %+v, want 1.25 exactly with a degenerate interval", a)
	}

	dd := bootstrapMetric(pnl, days, func(p []float64) (float64, bool) { return maxDrawdown(p), true }, c)
	if !dd.HasCI || dd.Lo > dd.Hi {
		t.Errorf("drawdown = %+v", dd)
	}
}

func TestBootstrapMetric_UndefinedStatIsUnreliable(t *testing.T) {
	pnl, days := seq(60, func(int) float64 { return 10 }) // no loss: PF undefined
	if m := bootstrapMetric(pnl, days, profitFactor, Criteria{MinSample: 30, MinClusters: 5}); m.Defined || m.Reliable {
		t.Errorf("PF without losses = %+v, want undefined / 判定不能", m)
	}
}
