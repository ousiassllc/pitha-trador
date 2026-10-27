package entryqualitytests

import (
	"database/sql"
	"math"
	"reflect"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/entryqualitydiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	labelV1 = 1
	labelV2 = 2
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// seedLabeled adds to seed's period the decisions that pass every guard at
// min_entry_quality good and the outcome labels the aggregation has to pick
// from, sort out or leave unlabeled. At good the candidates that pass all are
//
//	LONG : 3 (two v2 labels: the newer, +10, counts), 20 (-30), 21 (+40),
//	       22 (v2 row without a net return), 23 (v1 row only), 24 (no row)
//	SHORT: 7 (-20), 25 (0: not a win), 26 (+50), 27 (+10)
//
// Decision 15 (another prompt cohort) and 16 (outside the period) carry a label
// that must not be seen.
func seedLabeled(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := seed(t)
	good := answer{"good", 0.7, 0.1, 0.1}
	for _, f := range []fixture{
		{id: 20, direction: "LONG", ans: good},
		{id: 21, direction: "LONG", ans: good},
		{id: 22, direction: "LONG", ans: good},
		{id: 23, direction: "LONG", ans: good},
		{id: 24, direction: "LONG", ans: good},
		{id: 25, direction: "SHORT", ans: good},
		{id: 26, direction: "SHORT", ans: answer{"strong", 0.8, 0.1, 0.1}},
		{id: 27, direction: "SHORT", ans: good},
	} {
		insertFixture(t, w, f)
	}
	testkit.Outcome(t, w, 3, 5, labelV2, false, "sl", -50.0) // superseded by the next row (higher id)
	testkit.Outcome(t, w, 3, 10, labelV2, true, "timeout", 10.0)
	testkit.Outcome(t, w, 20, 10, labelV2, false, "sl", -30.0)
	testkit.Outcome(t, w, 21, 10, labelV2, true, "tp", 40.0)
	testkit.Outcome(t, w, 22, 10, labelV2, nil, nil, nil)
	testkit.Outcome(t, w, 23, 10, labelV1, true, nil, nil)
	testkit.Outcome(t, w, 7, 10, labelV2, false, "timeout", -20.0)
	testkit.Outcome(t, w, 25, 10, labelV2, nil, "timeout", 0.0)
	testkit.Outcome(t, w, 26, 15, labelV2, true, "tp", 50.0)
	testkit.Outcome(t, w, 27, 10, labelV2, true, "tp", 10.0)
	testkit.Outcome(t, w, 15, 10, labelV2, true, "tp", 1000.0)
	testkit.Outcome(t, w, 16, 10, labelV2, true, "tp", 1000.0)
	return path, w
}

// The outcomes of the candidates that pass every guard, worked out by hand.
func TestWhatIfOutcomesOfThePassingCandidates(t *testing.T) {
	path, _ := seedLabeled(t)
	d := newTestServer(t, path).call(t, args(map[string]any{"min_entry_quality": []string{"good"}})).data(t)
	o := scenario(t, d, "good").Outcomes

	l := o.Long
	if l.Candidates != 6 || l.Labeled != 3 || l.Unlabeled != 3 || l.Wins != 2 {
		t.Fatalf("long = %+v", l)
	}
	if l.WinRate == nil || !near(*l.WinRate, 2.0/3) {
		t.Errorf("long win rate = %v, want 2/3", l.WinRate)
	}
	n := l.NetReturn
	if n == nil || !near(n.Mean, 20.0/3) || !near(n.Median, 10) || !near(n.Sum, 20) {
		t.Fatalf("long net return = %+v, want mean 20/3, median 10, sum 20", n)
	}
	w := n.Worst
	if w.NetReturnBps != -30 || w.JevDecisionID != 20 || w.Symbol != "7203" || w.DecidedAt != "2026-10-15T01:00:00Z" || w.BarrierHit == nil || *w.BarrierHit != "sl" {
		t.Errorf("long worst = %+v, want decision 20 at -30 (sl)", w)
	}
	if want := []entryqualitydiag.HorizonCount{{Minutes: 10, Labeled: 3}}; !reflect.DeepEqual(l.Horizons, want) {
		t.Errorf("long horizons = %+v, want %+v", l.Horizons, want)
	}

	s := o.Short
	if s.Candidates != 4 || s.Labeled != 4 || s.Unlabeled != 0 || s.Wins != 2 {
		t.Fatalf("short = %+v", s)
	}
	if s.WinRate == nil || !near(*s.WinRate, 0.5) {
		t.Errorf("short win rate = %v, want 0.5 (a zero return is not a win)", s.WinRate)
	}
	if n := s.NetReturn; n == nil || !near(n.Mean, 10) || !near(n.Median, 5) || !near(n.Sum, 40) || n.Worst.JevDecisionID != 7 || n.Worst.NetReturnBps != -20 {
		t.Errorf("short net return = %+v, want mean 10, median 5 (even count), sum 40, worst decision 7", n)
	}
	if want := []entryqualitydiag.HorizonCount{{Minutes: 10, Labeled: 3}, {Minutes: 15, Labeled: 1}}; !reflect.DeepEqual(s.Horizons, want) {
		t.Errorf("short horizons = %+v, want %+v", s.Horizons, want)
	}

	if sc := scenario(t, d, "good"); o.Long.Candidates != sc.Long.PassAllGuards || o.Short.Candidates != sc.Short.PassAllGuards {
		t.Errorf("outcome candidates %d/%d differ from pass_all_guards %d/%d", o.Long.Candidates, o.Short.Candidates, sc.Long.PassAllGuards, sc.Short.PassAllGuards)
	}
}

// A candidate that passes at a lower minimum but not at a higher one is
// aggregated only where it passes.
func TestWhatIfOutcomesFollowEachThreshold(t *testing.T) {
	path, _ := seedLabeled(t)
	d := newTestServer(t, path).call(t, args(nil)).data(t)
	fair, strong := scenario(t, d, "fair").Outcomes, scenario(t, d, "strong").Outcomes

	// fair adds decision 2 (poor-looking signal, answer "fair") to good's LONG
	// candidates; it has no label.
	if fair.Long.Candidates != 7 || fair.Long.Labeled != 3 || fair.Long.Unlabeled != 4 {
		t.Errorf("fair long = %+v, want 7 candidates, 3 labeled", fair.Long)
	}
	// strong keeps only the strong answers: decisions 7 and 26 on the short side
	// and no LONG decision at all.
	if strong.Long.Candidates != 0 || strong.Long.WinRate != nil || strong.Long.NetReturn != nil || len(strong.Long.Horizons) != 0 {
		t.Errorf("strong long = %+v, want an empty, null summary", strong.Long)
	}
	if s := strong.Short; s.Candidates != 2 || s.Labeled != 2 || s.NetReturn == nil || !near(s.NetReturn.Sum, 30) || !near(s.NetReturn.Median, 15) {
		t.Errorf("strong short = %+v, want decisions 7 (-20) and 26 (+50)", s)
	}
}

// Without any outcome label every candidate is unlabeled and the statistics are
// null rather than zero.
func TestWhatIfOutcomesWithoutLabels(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, path).call(t, args(map[string]any{"min_entry_quality": []string{"good"}})).data(t)
	o := scenario(t, d, "good").Outcomes
	for side, got := range map[string]entryqualitydiag.Outcomes{"long": o.Long, "short": o.Short} {
		if got.Candidates != 1 || got.Labeled != 0 || got.Unlabeled != 1 || got.Wins != 0 || got.WinRate != nil || got.NetReturn != nil {
			t.Errorf("%s = %+v, want one unlabeled candidate and null statistics", side, got)
		}
	}
}

// Adding labels changes nothing but the new fields: the population, the
// distribution and every scenario count are the engine's verdicts, not
// something the labels filter.
func TestWhatIfCountsDoNotDependOnTheLabels(t *testing.T) {
	path, w := seed(t)
	srv := newTestServer(t, path)
	before := srv.call(t, args(nil)).data(t)

	testkit.Outcome(t, w, 3, 10, labelV2, false, "sl", -300.0)
	testkit.Outcome(t, w, 7, 10, labelV2, true, "tp", 300.0)
	testkit.Outcome(t, w, 2, 10, labelV2, true, "tp", 5.0)
	after := srv.call(t, args(nil)).data(t)

	if after.Scenarios[1].Outcomes.Long.Labeled == 0 {
		t.Fatal("the labels were not read")
	}
	strip := func(d entryqualitydiag.Data) entryqualitydiag.Data {
		scenarios := make([]entryqualitydiag.Scenario, len(d.Scenarios))
		copy(scenarios, d.Scenarios)
		for i := range scenarios {
			scenarios[i].Outcomes = entryqualitydiag.ScenarioOutcomes{}
		}
		d.Scenarios, d.LabelAssumptions = scenarios, entryqualitydiag.LabelAssumptions{}
		return d
	}
	if !reflect.DeepEqual(strip(before), strip(after)) {
		t.Errorf("labels changed the counts:\nbefore %+v\nafter  %+v", strip(before), strip(after))
	}
}

// The holding time, fee and slippage the labels were measured with are in the
// response: the shipped defaults, and the runtime_settings overrides when set.
func TestWhatIfLabelAssumptions(t *testing.T) {
	path, w := seed(t)
	srv := newTestServer(t, path)

	a := srv.call(t, args(nil)).data(t).LabelAssumptions
	if a.LabelVersion != 2 || a.TimeoutMinutes != 10 || a.TakeProfitBps != 30 || a.StopLossBps != 30 || a.BarrierCostBps != 10 ||
		a.LabelCostBps.Long != 10 || a.LabelCostBps.Short != 10 || a.FeeBps != 0 || a.FeeFixedJPY != 0 || a.ShortBorrowBps != 0 ||
		a.SlippageLong != (entryqualitydiag.SlippageBps{ContinuousBps: 2, OpeningBps: 5, ClosingBps: 5}) || a.SlippageShort != a.SlippageLong {
		t.Errorf("default assumptions = %+v", a)
	}
	if a.Selection == "" || a.Win == "" || a.Exit == "" || a.Settings == "" {
		t.Errorf("assumption texts missing: %+v", a)
	}

	testkit.Setting(t, w, "calibration.barrier_timeout_minutes", "15")
	testkit.Setting(t, w, "cost.fee_bps", "3")
	testkit.Setting(t, w, "cost.slippage_short_continuous_bps", "4")
	testkit.Setting(t, w, "cost.short_borrow_bps", "1.5")
	a = srv.call(t, args(nil)).data(t).LabelAssumptions
	// label cost = barrier cost + (round trip - default round trip):
	//   LONG  2*3 = 6 over the default 4: 10 + 6 = 10 + (2*3+2*2 - 2*2) = 16
	//   SHORT 2*3 + 2*4 + 1.5 = 15.5 over the default 4: 10 + 11.5 = 21.5
	if a.TimeoutMinutes != 15 || a.FeeBps != 3 || a.SlippageShort.ContinuousBps != 4 || a.ShortBorrowBps != 1.5 ||
		!near(a.LabelCostBps.Long, 16) || !near(a.LabelCostBps.Short, 21.5) {
		t.Errorf("overridden assumptions = %+v", a)
	}
}
