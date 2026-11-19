package labelcompare

import (
	"math"
	"slices"
	"testing"
	"time"

	"gonum.org/v1/gonum/stat/distuv"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

func ser(days map[int64][2]float64) series { // day -> {n, sum}
	s := series{}
	for k, v := range days {
		s.add(k, int(v[0]), v[1])
	}
	return s
}

func approx(got *float64, want, tol float64) bool { return got != nil && math.Abs(*got-want) <= tol }

// Two days per side, hand-calculated: the day means are [1, 3] and [5, 7].
//   - each mean: 2 and 6, interval = mean +- t(1) * SE, SE = 1 -> +-12.7062;
//   - difference 4, Welch: var 2 each, SE = sqrt(2/2 + 2/2) = sqrt(2), df = 2,
//     t(2) = 4.30265 -> half-width 6.0849.
func TestCompareIsTheDayClusteredIntervalAndWelchOnDayMeans(t *testing.T) {
	c := compare(ser(map[int64][2]float64{1: {2, 2}, 2: {1, 3}}), ser(map[int64][2]float64{3: {4, 20}, 4: {1, 7}}))
	if !approx(c.Before.MeanNetBps, 2, 1e-12) || !approx(c.After.MeanNetBps, 6, 1e-12) || c.Before.N != 3 || c.After.N != 5 || c.Before.Days != 2 {
		t.Fatalf("stats = %+v / %+v", c.Before, c.After)
	}
	if !approx(c.Before.CILow, 2-12.7062, 1e-3) || !approx(c.Before.CIHigh, 2+12.7062, 1e-3) {
		t.Errorf("before interval = %v..%v", *c.Before.CILow, *c.Before.CIHigh)
	}
	if c.Diff == nil || math.Abs(c.Diff.DiffBps-4) > 1e-12 || !approx(c.Diff.DF, 2, 1e-9) ||
		!approx(c.Diff.CILow, 4-6.0849, 1e-3) || !approx(c.Diff.CIHigh, 4+6.0849, 1e-3) {
		t.Errorf("diff = %+v", c.Diff)
	}
}

// One day cannot give an interval; no day gives no mean.
func TestCompareWithTooFewDays(t *testing.T) {
	one := ser(map[int64][2]float64{1: {3, 6}})
	c := compare(one, one)
	if !approx(c.Before.MeanNetBps, 2, 0) || c.Before.CILow != nil || c.Before.CIHigh != nil || c.Diff == nil || c.Diff.CILow != nil {
		t.Errorf("one day: %+v diff %+v", c.Before, c.Diff)
	}
	empty := compare(series{}, one)
	if empty.Before.MeanNetBps != nil || empty.Diff != nil || empty.After.Days != 1 {
		t.Errorf("no day: %+v", empty)
	}
}

// daysNeeded is the first g at which the half-width falls below |diff|.
func TestDaysNeeded(t *testing.T) {
	g, ok := daysNeeded(1, 1, 100)
	if !ok || g != 2 {
		t.Errorf("huge difference: g=%d ok=%v, want 2", g, ok)
	}
	g1, _ := daysNeeded(1, 1, 1)
	g2, _ := daysNeeded(1, 1, 0.5)
	g3, _ := daysNeeded(4, 4, 0.5)
	if g1 >= g2 || g2 >= g3 {
		t.Errorf("needed days must grow as the difference shrinks / the spread grows: %d %d %d", g1, g2, g3)
	}
	// Minimal: the half-width at g is below |diff| and at g-1 it is not.
	half := func(vb, va float64, g int) float64 {
		return distuv.StudentsT{Mu: 0, Sigma: 1, Nu: float64(2*g - 2)}.Quantile(0.975) * math.Sqrt((vb+va)/float64(g))
	}
	for _, c := range []struct{ vb, va, diff float64 }{{1, 1, 1}, {2, 3, 0.7}, {0.01, 0.02, -0.3}} {
		g, ok := daysNeeded(c.vb, c.va, c.diff)
		if !ok || half(c.vb, c.va, g) >= math.Abs(c.diff) || (g > 2 && half(c.vb, c.va, g-1) < math.Abs(c.diff)) {
			t.Errorf("%+v: g=%d ok=%v is not the first day count below |diff|", c, g, ok)
		}
	}
	if _, ok := daysNeeded(100, 100, 0.001); ok {
		t.Error("a difference that needs more than MaxDaysNeeded days must not be found")
	}
}

func TestNeededReportsRemainingDaysOrAReason(t *testing.T) {
	before := ser(map[int64][2]float64{1: {1, 0}, 2: {1, 1}, 3: {1, 0}})
	after := ser(map[int64][2]float64{4: {1, 5}, 5: {1, 6}, 6: {1, 5}})
	d := needed([2]series{before, after}, compare(before, after))
	if d.Reason != "" || d.NeededPerSide == nil || *d.NeededPerSide != MinDaysFloor || d.ObservedPerSide != 3 || d.DaysToMin != 2 || d.RemainingDays == nil || *d.RemainingDays != 2 {
		t.Errorf("needed = %+v", d)
	}

	same := ser(map[int64][2]float64{1: {1, 0}, 2: {1, 1}})
	if d := needed([2]series{same, same}, compare(same, same)); d.Reason != ReasonDiffNearZero || d.RemainingDays != nil || d.DaysToMin != 3 {
		t.Errorf("equal windows: %+v", d)
	}
	oneDay := ser(map[int64][2]float64{1: {1, 0}})
	if d := needed([2]series{oneDay, oneDay}, compare(oneDay, oneDay)); d.Reason != ReasonVarianceUndefined || d.RemainingDays != nil {
		t.Errorf("one day: %+v", d)
	}
	noisy := ser(map[int64][2]float64{1: {1, -100}, 2: {1, 100}, 3: {1, -100}, 4: {1, 100}, 5: {1, 0}})
	shifted := ser(map[int64][2]float64{6: {1, -99.999}, 7: {1, 100.001}, 8: {1, -99.999}, 9: {1, 100.001}, 10: {1, 0.001}})
	if d := needed([2]series{noisy, shifted}, compare(noisy, shifted)); d.Reason != ReasonBeyondHorizon || d.NeededPerSide != nil {
		t.Errorf("a tiny shift in a noisy series: %+v", d)
	}
}

func TestLabelVerdictRules(t *testing.T) {
	low, high, zero := new(-1.0), new(1.0), new(0.0)
	enough := &DaysNeeded{DaysToMin: 0}
	short := &DaysNeeded{DaysToMin: 2}
	diff := func(lo, hi *float64) SideCompare { return SideCompare{Diff: &Diff{CILow: lo, CIHigh: hi}} }
	for name, c := range map[string]struct {
		withheld string
		sc       SideCompare
		d        *DaysNeeded
		want     string
		judged   bool
	}{
		"aborted beats everything": {VerdictAborted, diff(high, high), short, VerdictAborted, false},
		"confounded beats days":    {VerdictConfounded, diff(high, high), short, VerdictConfounded, false},
		"too few days":             {"", diff(high, high), short, VerdictInsufficientDays, false},
		"switch unconfirmed":       {VerdictSwitchUnconfirmed, diff(high, high), enough, VerdictSwitchUnconfirmed, false},
		"no interval":              {"", SideCompare{Diff: &Diff{}}, enough, VerdictNotComputable, false},
		"no diff":                  {"", SideCompare{}, enough, VerdictNotComputable, false},
		"interval wholly above 0":  {"", diff(new(0.1), high), enough, VerdictImproved, true},
		"interval wholly below 0":  {"", diff(low, new(-0.1)), enough, VerdictWorsened, true},
		"interval across 0":        {"", diff(low, high), enough, VerdictInconclusive, true},
		"a lower end of exactly 0": {"", diff(zero, high), enough, VerdictInconclusive, true},
	} {
		got := labelVerdict(c.withheld, c.sc, c.d)
		if got != c.want || judged(got) != c.judged {
			t.Errorf("%s: %s (judged %v), want %s (%v)", name, got, judged(got), c.want, c.judged)
		}
	}
}

func TestWholeDays(t *testing.T) {
	jst := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, query.JST) }
	for name, c := range map[string]struct {
		r           query.Range
		first, last string
		ok          bool
	}{
		"day boundaries":    {query.Range{From: jst(7, 0), To: jst(12, 0)}, "2026-10-07", "2026-10-11", true},
		"partial edge days": {query.Range{From: jst(7, 9), To: jst(12, 9)}, "2026-10-08", "2026-10-11", true},
		"inside one day":    {query.Range{From: jst(7, 9), To: jst(7, 15)}, "", "", false},
		"one whole day":     {query.Range{From: jst(7, 0), To: jst(8, 0)}, "2026-10-07", "2026-10-07", true},
		"empty":             {query.Range{From: jst(7, 0), To: jst(7, 0)}, "", "", false},
	} {
		first, last, ok := wholeDays(c.r)
		if first != c.first || last != c.last || ok != c.ok {
			t.Errorf("%s: %s..%s %v, want %s..%s %v", name, first, last, ok, c.first, c.last, c.ok)
		}
	}
}

func TestSkipsOfUsesFixedCodesAndCountsEachSkipOnce(t *testing.T) {
	if skipsOf(nil) != nil {
		t.Fatal("a window whose rejections were not read has no skips")
	}
	s := skipsOf(&query.RiskRejections{
		RiskPassedCount: 6, OtherRejectCount: 7,
		Buckets: []query.RejectBucket{{Reason: "max_daily_trades", Count: 3}},
		OtherRejectBreakdown: []query.RejectBucket{
			{Reason: "jev_none:regime=RANGE", Count: 4}, {Reason: "meta_skip:no_active_model", Count: 2},
			{Reason: "jev_none:conf=ge_0.8", Count: 4}, {Reason: "unknown", Count: 1},
		},
	})
	var codes []string
	for _, r := range s.TopReasons {
		codes = append(codes, r.Reason)
	}
	if s.SkipCount != 10 || s.RiskPassedCount != 6 || s.SkipRate == nil || math.Abs(*s.SkipRate-10.0/16) > 1e-12 {
		t.Errorf("skips = %+v", s)
	}
	if want := []string{"jev_none:regime=RANGE", "risk_engine_rejected:max_daily_trades", "meta_skip:no_active_model", "unknown"}; !slices.Equal(codes, want) {
		t.Errorf("codes = %v, want %v", codes, want)
	}
	if s := skipsOf(&query.RiskRejections{}); s.SkipRate != nil || s.TopReasons == nil || len(s.TopReasons) != 0 {
		t.Errorf("no signal: %+v", s)
	}
}

// The experiment-level state that withholds judgement is taken from the trade
// comparison's own result, in the trade verdict's order.
func TestInputOfWithholdsLikeTheTradeVerdict(t *testing.T) {
	var c experiment.Comparison
	c.Switch.Status = experiment.SwitchConfirmed
	if in := InputOf(c, query.Range{}, query.Range{}); in.Withheld != "" {
		t.Errorf("clean: %q", in.Withheld)
	}
	c.Switch.Status = experiment.SwitchMismatch
	if in := InputOf(c, query.Range{}, query.Range{}); in.Withheld != VerdictSwitchUnconfirmed {
		t.Errorf("mismatch: %q", in.Withheld)
	}
	c.Confounded = true
	if in := InputOf(c, query.Range{}, query.Range{}); in.Withheld != VerdictConfounded {
		t.Errorf("confounded: %q", in.Withheld)
	}
	c.Experiment.Status = experiment.StatusAborted
	if in := InputOf(c, query.Range{}, query.Range{}); in.Withheld != VerdictAborted {
		t.Errorf("aborted: %q", in.Withheld)
	}
}
