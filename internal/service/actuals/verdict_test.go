package actuals

import "testing"

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
