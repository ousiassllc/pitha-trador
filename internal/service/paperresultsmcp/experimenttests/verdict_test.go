package experimenttests

import (
	"database/sql"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Trades planted for the judged cases. The judgement needs 64 closed trades a
// side, and newServer already seeds 2 closed trades in the before period
// (+1000, -3000) and 3 in the after period (+2000, 0, -500), so planting 62
// and 61 makes exactly 64 each. A trade is closed a minute after the previous
// one, so the order (and so the drawdown) is the order of the slice.
const (
	plantedBeforeStart = "2026-10-01T02:00:00Z" // JST 10-01, before the seeded -3000 at 05:00Z
	plantedAfterStart  = "2026-10-03T03:00:00Z" // JST 10-03, after the seeded -500 at 02:00Z
)

func plant(t *testing.T, w *sql.DB, start string, pnls []float64) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatal(err)
	}
	for i, pnl := range pnls {
		testkit.Position(t, w, at.Add(time.Duration(i)*time.Minute).Format(time.RFC3339), &pnl, 1000, 100, nil, 99, nil)
	}
}

func plantPeriods(t *testing.T, w *sql.DB, before, after []float64) {
	t.Helper()
	plant(t, w, plantedBeforeStart, before)
	plant(t, w, plantedAfterStart, after)
}

// spread is n trades alternating mean+d / mean-d.
func spread(n int, mean, d float64) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = mean + d
		if i%2 == 1 {
			xs[i] = mean - d
		}
	}
	return xs
}

func repeat(v float64, n int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = v
	}
	return xs
}

func sum(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}

func plantedBefore() []float64 { return spread(62, 100, 200) }
func plantedAfter() []float64  { return spread(61, 1100, 200) }

// plantImproved plants a clear win at exactly 64 trades a side: the mean pnl
// per trade rises by about 1000 and the maximum drawdown does not.
func plantImproved(t *testing.T, w *sql.DB) {
	t.Helper()
	plantPeriods(t, w, plantedBefore(), plantedAfter())
}

// create -> list -> compare: the experiment is recorded with its hypothesis and
// one variable, shows in experiments_list with its trade counts, and
// experiment_compare returns, for both periods, exactly what
// paper_performance_summary and risk_rejections return for the same days.
func TestCompareMatchesPerformanceSummaryAndRiskRejections(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	if e.ID != 1 || e.Status != experiment.StatusRunning || e.Actor != experiment.ActorMCP || e.Variable.Kind != experiment.KindSetting ||
		e.Variable.Name != "risk.paper.max_trade_loss_pct" || e.Variable.Before != "1" || e.Variable.After != "0.5" || e.EndedAt != nil {
		t.Fatalf("created experiment = %+v", e)
	}
	closeAt(t, srv, e.ID, endedAt)

	var list struct {
		Experiments []experiment.Summary `json:"experiments"`
	}
	srv.call(t, "experiments_list", nil).data(t, "experiments_list", &list)
	if len(list.Experiments) != 1 {
		t.Fatalf("experiments_list = %+v, want the one experiment", list)
	}
	row := list.Experiments[0]
	if row.Experiment.ID != e.ID || row.Experiment.Status != experiment.StatusClosed || row.BeforeTrades != 64 || row.AfterTrades != 64 ||
		row.Verdict != experiment.VerdictImproved || !row.Judged {
		t.Errorf("list row = %+v, want closed, 64 trades before and 64 after, judged improved", row)
	}

	c := compare(t, srv, e.ID, nil)
	var beforePerf, afterPerf query.Performance
	srv.call(t, "paper_performance_summary", beforeDays).data(t, "paper_performance_summary", &beforePerf)
	srv.call(t, "paper_performance_summary", afterDays).data(t, "paper_performance_summary", &afterPerf)
	var beforeRisk, afterRisk query.RiskRejections
	srv.call(t, "risk_rejections", beforeDays).data(t, "risk_rejections", &beforeRisk)
	srv.call(t, "risk_rejections", afterDays).data(t, "risk_rejections", &afterRisk)

	if !reflect.DeepEqual(c.Before.Performance, beforePerf) || !reflect.DeepEqual(c.After.Performance, afterPerf) {
		t.Errorf("performance differs from paper_performance_summary:\n before %+v vs %+v\n after  %+v vs %+v", c.Before.Performance, beforePerf, c.After.Performance, afterPerf)
	}
	if !reflect.DeepEqual(c.Before.RiskRejections, &beforeRisk) || !reflect.DeepEqual(c.After.RiskRejections, &afterRisk) {
		t.Errorf("risk rejections differ from risk_rejections:\n before %+v vs %+v\n after  %+v vs %+v", c.Before.RiskRejections, &beforeRisk, c.After.RiskRejections, &afterRisk)
	}
	// The seeded trades are in the periods too: before = +1000 and -3000, after = +2000, 0 and -500.
	wantBefore, wantAfter := seededBefore(), seededAfter()
	if b, a := c.Before.Performance, c.After.Performance; b.ClosedTradeCount != len(wantBefore) || b.TotalPnlJPY != sum(wantBefore) ||
		a.ClosedTradeCount != len(wantAfter) || a.TotalPnlJPY != sum(wantAfter) {
		t.Errorf("figures: before %+v, after %+v", b, a)
	}
	if len(beforeRisk.OtherRejectBreakdown) == 0 || len(afterRisk.OtherRejectBreakdown) != 0 {
		t.Errorf("reject breakdowns: before %v, after %v; the seeded signals all fall before the start", beforeRisk.OtherRejectBreakdown, afterRisk.OtherRejectBreakdown)
	}
	// Average win / loss are in both periods (positive win, negative loss).
	if p := c.Before.Performance; p.AvgWinJPY == nil || *p.AvgWinJPY <= 0 || p.AvgLossJPY == nil || *p.AvgLossJPY >= 0 {
		t.Errorf("before avg win / loss = %v / %v, want a positive win and a negative loss", p.AvgWinJPY, p.AvgLossJPY)
	}
	if c.Verdict != experiment.VerdictImproved || !c.Judged || c.Confounded {
		t.Errorf("verdict = %s judged=%v confounded=%v", c.Verdict, c.Judged, c.Confounded)
	}
}

func seededBefore() []float64 { return append([]float64{1000, -3000}, plantedBefore()...) }
func seededAfter() []float64  { return append([]float64{2000, 0, -500}, plantedAfter()...) }

// 同じ取引集合・同じ法: the per-trade series the test runs on is the set
// paper_performance_summary counted: the count and the mean are the summary's
// closed_trade_count and expectancy_jpy, to the last digit.
func TestPerTradeSeriesIsThePerformanceSummarysSet(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)
	c := compare(t, srv, e.ID, nil)

	for name, tc := range map[string]struct {
		days   map[string]any
		sample experiment.Sample
		want   []float64
	}{
		"before": {beforeDays, c.PerTradePnl.Before, seededBefore()},
		"after":  {afterDays, c.PerTradePnl.After, seededAfter()},
	} {
		var perf query.Performance
		srv.call(t, "paper_performance_summary", tc.days).data(t, "paper_performance_summary", &perf)
		if tc.sample.N != perf.ClosedTradeCount || perf.ExpectancyJPY == nil || tc.sample.MeanJPY == nil || *tc.sample.MeanJPY != *perf.ExpectancyJPY {
			t.Errorf("%s: sample n=%d mean=%v, summary closed_trade_count=%d expectancy_jpy=%v", name, tc.sample.N, tc.sample.MeanJPY, perf.ClosedTradeCount, perf.ExpectancyJPY)
		}
		if got := experiment.Welch(tc.want, tc.want).Before; got.N != tc.sample.N || *got.MeanJPY != *tc.sample.MeanJPY || *got.SDJPY != *tc.sample.SDJPY {
			t.Errorf("%s: sample %+v differs from the planted series %+v", name, tc.sample, got)
		}
	}
	if p := c.PerTradePnl; p.CILevel != 0.95 || !p.Computable() || p.T == nil || p.DF == nil || p.Diff == nil {
		t.Errorf("per_trade_pnl = %+v, want ci_level 0.95 and a computed interval, t, df and diff", p)
	}
}

// 判定: the interval and the drawdown decide, end to end. Only an interval
// wholly above 0 with the drawdown no worse is called improved.
func TestVerdictFollowsTheIntervalAndTheDrawdown(t *testing.T) {
	wins := repeat(5000, 51)
	losses := repeat(-8000, 10)
	tests := []struct {
		name          string
		before, after []float64
		want          string
	}{
		{"interval above 0, drawdown not worse: win", spread(62, 100, 200), spread(61, 1100, 200), experiment.VerdictImproved},
		{"interval above 0 but drawdown worse: not a win", spread(62, 100, 200), append(slices.Clone(losses), wins...), experiment.VerdictMixed},
		{"interval crosses 0: not a win", spread(62, 100, 1000), spread(61, 100, 1000), experiment.VerdictInconclusive},
		{"interval below 0", spread(62, 1100, 200), spread(61, 100, 200), experiment.VerdictWorsened},
		{"63 trades in the before period: withheld", spread(61, 100, 200), spread(61, 1100, 200), experiment.VerdictInsufficientTrades},
		{"63 trades in the after period: withheld", spread(62, 100, 200), spread(60, 1100, 200), experiment.VerdictInsufficientTrades},
	}
	for _, tt := range tests {
		srv, w := newServer(t)
		plantPeriods(t, w, tt.before, tt.after)
		e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
		closeAt(t, srv, e.ID, endedAt)
		c := compare(t, srv, e.ID, nil)
		if c.Verdict != tt.want {
			t.Errorf("%s: verdict %s, want %s (n %d/%d, diff %v, interval %v..%v, drawdown %v -> %v)", tt.name, c.Verdict, tt.want,
				c.Before.Performance.ClosedTradeCount, c.After.Performance.ClosedTradeCount, c.PerTradePnl.Diff, c.PerTradePnl.CILow, c.PerTradePnl.CIHigh,
				c.Before.Performance.MaxDrawdownJPY, c.After.Performance.MaxDrawdownJPY)
		}
		if wantJudged := tt.want != experiment.VerdictInsufficientTrades; c.Judged != wantJudged {
			t.Errorf("%s: judged = %v, want %v", tt.name, c.Judged, wantJudged)
		}
		if c.PerTradePnl.Before.N == 0 || c.PerTradePnl.After.N == 0 || c.PerTradePnl.Diff == nil {
			t.Errorf("%s: the figures must be returned even when judgement is withheld: %+v", tt.name, c.PerTradePnl)
		}
	}
}

// 最低件数前は判定なし, and the floor: below min_trades (never below 64) the
// figures are returned and no call is made; the floor is the unapproved
// proposal; a caller may raise it but a lower value is refused, not replaced.
func TestNoVerdictBeforeTheMinimumNumberOfTrades(t *testing.T) {
	srv, w := newServer(t)
	plantImproved(t, w)
	e := create(t, srv, createArgs("risk.paper.max_trade_loss_pct", nil))
	closeAt(t, srv, e.ID, endedAt)

	c := compare(t, srv, e.ID, nil)
	if c.MinTrades != experiment.MinTradesFloor || c.MinTradesSource != experiment.MinTradesSourceFloor {
		t.Errorf("default min_trades = %d (%s), want the provisional floor %d", c.MinTrades, c.MinTradesSource, experiment.MinTradesFloor)
	}
	if c.Verdict != experiment.VerdictImproved {
		t.Fatalf("verdict = %s, want improved at exactly 64 trades a side", c.Verdict)
	}
	// Raising it above what a period has withholds judgement but keeps the figures.
	raised := compare(t, srv, e.ID, map[string]any{"min_trades": 65})
	if raised.Verdict != experiment.VerdictInsufficientTrades || raised.Judged || raised.MinTrades != 65 || raised.MinTradesSource != experiment.MinTradesSourceRaised {
		t.Errorf("min_trades 65: verdict %s judged=%v min_trades=%d (%s), want insufficient_trades, raised", raised.Verdict, raised.Judged, raised.MinTrades, raised.MinTradesSource)
	}
	if raised.After.Performance.ClosedTradeCount != 64 || raised.PerTradePnl.After.N != 64 {
		t.Errorf("the figures must still be returned: %+v", raised.After.Performance)
	}
	// Asking for exactly the floor is the floor: judged, source still the provisional one.
	if got := compare(t, srv, e.ID, map[string]any{"min_trades": experiment.MinTradesFloor}); got.Verdict != experiment.VerdictImproved || got.MinTradesSource != experiment.MinTradesSourceFloor {
		t.Errorf("min_trades %d: verdict %s (%s)", experiment.MinTradesFloor, got.Verdict, got.MinTradesSource)
	}
	// Lower is refused with invalid_value (not silently raised), in both tools.
	for _, bad := range []any{experiment.MinTradesFloor - 1, 2, 1, 0, -1, experiment.MaxMinTrades + 1} {
		for _, tool := range []string{"experiment_compare", "experiments_list"} {
			args := map[string]any{"min_trades": bad}
			if tool == "experiment_compare" {
				args["experiment_id"] = e.ID
			}
			r := srv.call(t, tool, args)
			r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
			if bad == experiment.MinTradesFloor-1 && !strings.Contains(string(r.body), "min_trades=63") {
				t.Errorf("%s: the refusal does not say why: %s", tool, r.body)
			}
		}
	}
}
