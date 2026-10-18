package actuals

import (
	"math"
	"slices"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/actuals"
)

// goodStats is a result that passes every check of the default 合格ライン
// exactly at the boundary.
func goodStats() Stats {
	c := DefaultCriteria()
	return Stats{
		N: c.MinTrades, Days: c.MinDays,
		EVBps:          Metric{N: c.MinTrades, Value: 3, Lo: 0.5, Hi: 5.5, HasCI: true, Defined: true, Reliable: true},
		MaxDrawdownPct: Metric{N: c.MinTrades, Value: c.MaxDrawdownPct, Defined: true, Reliable: true},
	}
}

func status(v Verdict, key string) Status {
	for _, c := range v.Checks {
		if c.Key == key {
			return c.Status
		}
	}
	return ""
}

func TestJudge_Boundaries(t *testing.T) {
	c := DefaultCriteria()
	cases := []struct {
		name   string
		mutate func(*Stats)
		want   Status
		key    string
		check  Status
	}{
		{"all at the boundary pass", func(*Stats) {}, StatusPass, CheckTrades, StatusPass},
		{"99 trades is 判定不能", func(s *Stats) { s.N = c.MinTrades - 1 }, StatusUndetermined, CheckTrades, StatusUndetermined},
		{"19 days is 判定不能", func(s *Stats) { s.Days = c.MinDays - 1 }, StatusUndetermined, CheckPeriodDays, StatusUndetermined},
		{"EV lower bound exactly 0 fails", func(s *Stats) { s.EVBps.Lo = 0 }, StatusFail, CheckEVLowerBound, StatusFail},
		{"EV lower bound just above 0 passes", func(s *Stats) { s.EVBps.Lo = 1e-9 }, StatusPass, CheckEVLowerBound, StatusPass},
		{"unreliable EV is 判定不能", func(s *Stats) { s.EVBps.Reliable = false }, StatusUndetermined, CheckEVLowerBound, StatusUndetermined},
		{"drawdown exactly at the cap passes", func(s *Stats) { s.MaxDrawdownPct.Value = c.MaxDrawdownPct }, StatusPass, CheckMaxDrawdown, StatusPass},
		{"drawdown above the cap fails", func(s *Stats) { s.MaxDrawdownPct.Value = c.MaxDrawdownPct + 0.01 }, StatusFail, CheckMaxDrawdown, StatusFail},
		{"drawdown breach fails even with little data", func(s *Stats) {
			s.N, s.Days = 3, 1
			s.MaxDrawdownPct.Value = 50
		}, StatusFail, CheckMaxDrawdown, StatusFail},
		{"within the cap with little data is 判定不能", func(s *Stats) { s.N, s.Days = 3, 1 }, StatusUndetermined, CheckMaxDrawdown, StatusUndetermined},
		{"no capital is 判定不能", func(s *Stats) { s.MaxDrawdownPct = Metric{} }, StatusUndetermined, CheckMaxDrawdown, StatusUndetermined},
	}
	for _, tc := range cases {
		s := goodStats()
		tc.mutate(&s)
		v := judge(s, 1_000_000, c)
		if v.Status != tc.want || status(v, tc.key) != tc.check || len(v.Checks) != 4 {
			t.Errorf("%s: verdict %v, %s=%v; want %v, %v", tc.name, v.Status, tc.key, status(v, tc.key), tc.want, tc.check)
		}
	}
}

// A failed check wins over a 判定不能 one.
func TestJudge_FailBeatsUndetermined(t *testing.T) {
	s := goodStats()
	s.N = 5
	s.MaxDrawdownPct.Value = 99
	if v := judge(s, 1, DefaultCriteria()); v.Status != StatusFail {
		t.Errorf("verdict = %v, want fail", v.Status)
	}
}

// statusesOf is the status of the four checks in their reported order
// (period_days, trades, ev_lower_bound, max_drawdown).
func statusesOf(v Verdict) [4]Status {
	var out [4]Status
	for i, c := range v.Checks {
		out[i] = c.Status
	}
	return out
}

// goodStatsFor is goodStats for any Criteria: exactly at every threshold.
func goodStatsFor(c Criteria) Stats {
	s := goodStats()
	s.N, s.Days = c.MinTrades, c.MinDays
	s.EVBps.N, s.MaxDrawdownPct.N = c.MinTrades, c.MinTrades
	s.EVBps.Lo = math.Nextafter(c.MinEVLowerBps, math.Inf(1))
	s.MaxDrawdownPct.Value = c.MaxDrawdownPct
	return s
}

// Every threshold is compared at exactly the boundary (one ulp below, on it,
// one ulp above), with the default criteria and with different ones so the
// verdict follows the Criteria fields instead of hard-coded numbers. Each row
// moves one input only and pins all four checks, so a failing criterion alone
// flips the verdict and the failed check is identified by its position/key.
func TestJudge_ThresholdBoundaries(t *testing.T) {
	const (
		P = StatusPass
		F = StatusFail
		U = StatusUndetermined
	)
	custom := Criteria{MinSample: 30, MinClusters: 5, MinDays: 3, MinTrades: 10, MinEVLowerBps: 2.5, MaxDrawdownPct: 1.5}
	for _, c := range []Criteria{DefaultCriteria(), custom} {
		below := func(x float64) float64 { return math.Nextafter(x, math.Inf(-1)) }
		above := func(x float64) float64 { return math.Nextafter(x, math.Inf(1)) }
		cases := []struct {
			name   string
			mutate func(*Stats)
			want   Status
			checks [4]Status
		}{
			{"every threshold exactly met", func(*Stats) {}, P, [4]Status{P, P, P, P}},
			{"days above the minimum", func(s *Stats) { s.Days = c.MinDays + 1 }, P, [4]Status{P, P, P, P}},
			{"trades above the minimum", func(s *Stats) { s.N = c.MinTrades + 1 }, P, [4]Status{P, P, P, P}},
			{"days one below: only that check and what needs enough data are 判定不能", func(s *Stats) { s.Days = c.MinDays - 1 }, U, [4]Status{U, P, U, U}},
			{"trades one below: only that check and what needs enough data are 判定不能", func(s *Stats) { s.N = c.MinTrades - 1 }, U, [4]Status{P, U, U, U}},
			{"EV lower bound one ulp below the minimum fails", func(s *Stats) { s.EVBps.Lo = below(c.MinEVLowerBps) }, F, [4]Status{P, P, F, P}},
			{"EV lower bound exactly the minimum fails (strict)", func(s *Stats) { s.EVBps.Lo = c.MinEVLowerBps }, F, [4]Status{P, P, F, P}},
			{"EV lower bound one ulp above the minimum passes", func(s *Stats) { s.EVBps.Lo = above(c.MinEVLowerBps) }, P, [4]Status{P, P, P, P}},
			{"EV lower bound far below fails", func(s *Stats) { s.EVBps.Lo = c.MinEVLowerBps - 100 }, F, [4]Status{P, P, F, P}},
			{"EV not reliable is 判定不能, not a failure, even below the minimum", func(s *Stats) {
				s.EVBps.Reliable = false
				s.EVBps.Lo = c.MinEVLowerBps - 100
			}, U, [4]Status{P, P, U, P}},
			{"EV below the minimum is not judged without enough days", func(s *Stats) {
				s.Days = c.MinDays - 1
				s.EVBps.Lo = c.MinEVLowerBps - 100
			}, U, [4]Status{U, P, U, U}},
			{"EV below the minimum is not judged without enough trades", func(s *Stats) {
				s.N = c.MinTrades - 1
				s.EVBps.Lo = c.MinEVLowerBps - 100
			}, U, [4]Status{P, U, U, U}},
			{"drawdown one ulp below the cap passes", func(s *Stats) { s.MaxDrawdownPct.Value = below(c.MaxDrawdownPct) }, P, [4]Status{P, P, P, P}},
			{"drawdown exactly the cap passes (inclusive)", func(s *Stats) { s.MaxDrawdownPct.Value = c.MaxDrawdownPct }, P, [4]Status{P, P, P, P}},
			{"drawdown one ulp above the cap fails", func(s *Stats) { s.MaxDrawdownPct.Value = above(c.MaxDrawdownPct) }, F, [4]Status{P, P, P, F}},
			{"drawdown breach fails without enough days", func(s *Stats) {
				s.Days = c.MinDays - 1
				s.MaxDrawdownPct.Value = above(c.MaxDrawdownPct)
			}, F, [4]Status{U, P, U, F}},
			{"drawdown breach fails without enough trades", func(s *Stats) {
				s.N = c.MinTrades - 1
				s.MaxDrawdownPct.Value = above(c.MaxDrawdownPct)
			}, F, [4]Status{P, U, U, F}},
			{"drawdown within the cap needs enough days", func(s *Stats) {
				s.Days = c.MinDays - 1
				s.MaxDrawdownPct.Value = c.MaxDrawdownPct
			}, U, [4]Status{U, P, U, U}},
			{"drawdown not defined (no capital) is 判定不能, not a failure", func(s *Stats) { s.MaxDrawdownPct = Metric{} }, U, [4]Status{P, P, P, U}},
			{"a failed EV beats an undefined drawdown", func(s *Stats) {
				s.EVBps.Lo = c.MinEVLowerBps
				s.MaxDrawdownPct = Metric{}
			}, F, [4]Status{P, P, F, U}},
			{"a drawdown breach beats an unreliable EV", func(s *Stats) {
				s.EVBps.Reliable = false
				s.MaxDrawdownPct.Value = above(c.MaxDrawdownPct)
			}, F, [4]Status{P, P, U, F}},
			// The drawdown check only reads the point estimate against the cap.
			{"drawdown within the cap passes whatever its interval's reliability", func(s *Stats) { s.MaxDrawdownPct.Reliable = false }, P, [4]Status{P, P, P, P}},
		}
		for _, tc := range cases {
			s := goodStatsFor(c)
			tc.mutate(&s)
			v := judge(s, 1_000_000, c)
			if v.Status != tc.want || statusesOf(v) != tc.checks {
				t.Errorf("%+v / %s: verdict %v, checks %v; want %v, %v", c, tc.name, v.Status, statusesOf(v), tc.want, tc.checks)
			}
		}
	}
}

// Each check reports its own threshold and the observed figure (the screen and
// API show "実績／基準"), identified by a stable key in a stable order.
func TestJudge_ChecksReportThresholdAndActual(t *testing.T) {
	c := Criteria{MinSample: 30, MinClusters: 5, MinDays: 3, MinTrades: 10, MinEVLowerBps: 2.5, MaxDrawdownPct: 1.5}
	s := goodStatsFor(c)
	s.Days, s.N = 4, 12
	s.EVBps.Lo = 7
	s.MaxDrawdownPct.Value = 1.25

	want := []Check{
		{Key: CheckPeriodDays, Required: 3, Actual: 4, HasActual: true, Status: StatusPass},
		{Key: CheckTrades, Required: 10, Actual: 12, HasActual: true, Status: StatusPass},
		{Key: CheckEVLowerBound, Required: 2.5, Actual: 7, HasActual: true, Status: StatusPass},
		{Key: CheckMaxDrawdown, Required: 1.5, Actual: 1.25, HasActual: true, Status: StatusPass},
	}
	if got := judge(s, 1_000_000, c).Checks; !slices.Equal(got, want) {
		t.Errorf("checks = %+v, want %+v", got, want)
	}

	// Without an interval / capital there is no actual figure to show.
	s.EVBps.HasCI = false
	s.EVBps.Reliable = false
	s.MaxDrawdownPct = Metric{}
	got := judge(s, 0, c).Checks
	if got[2].HasActual || got[2].Status != StatusUndetermined || got[2].Required != 2.5 {
		t.Errorf("EV without an interval = %+v, want no actual, 判定不能, required 2.5", got[2])
	}
	if got[3].HasActual || got[3].Status != StatusUndetermined || got[3].Required != 1.5 {
		t.Errorf("drawdown without capital = %+v, want no actual, 判定不能, required 1.5", got[3])
	}
}

// The drawdown ratio is the JPY drawdown over initial_capital, so the cap is
// met or breached by exactly the JPY amount that equals / exceeds
// MaxDrawdownPct of the capital, through the real aggregation.
func TestJudge_DrawdownCapThroughSummarize(t *testing.T) {
	c := DefaultCriteria()
	const capital = 30_000_000.0
	capJPY := capital * c.MaxDrawdownPct / 100 // 1,500,000

	// MinTrades trades over MinDays JST days: a first loss of dd (a drawdown
	// from the start), then steady gains that keep the EV interval above 0.
	book := func(dd float64) []repo.Trade {
		start := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC) // 10:00 JST
		trades := make([]repo.Trade, c.MinTrades)
		for i := range trades {
			pnl := 100_000.0
			if i == 0 {
				pnl = -dd
			}
			trades[i] = repo.Trade{
				PositionID: int64(i + 1), Side: "LONG", Quantity: 100, EntryPrice: 1000, RealizedPnL: pnl,
				ClosedAt: start.AddDate(0, 0, i%c.MinDays).Add(time.Duration(i/c.MinDays) * time.Minute),
			}
		}
		slices.SortStableFunc(trades, func(a, b repo.Trade) int { return a.ClosedAt.Compare(b.ClosedAt) })
		return trades
	}
	cases := []struct {
		name    string
		dd      float64
		capital float64
		want    Status
		check   Status
	}{
		{"1 JPY under the cap passes", capJPY - 1, capital, StatusPass, StatusPass},
		{"exactly the cap passes", capJPY, capital, StatusPass, StatusPass},
		{"1 JPY over the cap fails", capJPY + 1, capital, StatusFail, StatusFail},
		{"no capital is 判定不能 even with a drawdown over the cap", capJPY * 1.2, 0, StatusUndetermined, StatusUndetermined},
	}
	for _, tc := range cases {
		s := summarize(book(tc.dd), tc.capital, c)
		if s.Days != c.MinDays || s.N != c.MinTrades || !s.EVBps.Reliable || s.EVBps.Lo <= c.MinEVLowerBps {
			t.Fatalf("%s: fixture is not a sufficient, positive-EV sample: %+v", tc.name, s)
		}
		v := judge(s, tc.capital, c)
		// With a sufficient, positive-EV sample the drawdown alone decides.
		if v.Status != tc.want || status(v, CheckMaxDrawdown) != tc.check {
			t.Errorf("%s: verdict %v, drawdown %v (%.12g%%); want %v, %v", tc.name, v.Status, status(v, CheckMaxDrawdown), s.MaxDrawdownPct.Value, tc.want, tc.check)
		}
	}
}
