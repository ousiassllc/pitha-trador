package researchtests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchdiag"
)

func horizon(t *testing.T, d researchdiag.Data, name string) researchdiag.HorizonStats {
	t.Helper()
	for _, h := range d.Horizons {
		if h.Horizon == name {
			return h
		}
	}
	t.Fatalf("no horizon %s in %+v", name, d.Horizons)
	return researchdiag.HorizonStats{}
}

func wantSide(t *testing.T, name string, got *researchdiag.SideStats, rows, days int, gross, net, win, medGross, medNet float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: side is null", name)
	}
	if got.Rows != rows || got.Days != days {
		t.Errorf("%s: rows/days = %d/%d, want %d/%d", name, got.Rows, got.Days, rows, days)
	}
	for field, pair := range map[string][2]float64{
		"mean_gross": {got.MeanGrossBps, gross}, "mean_net": {got.MeanNetBps, net}, "win_rate": {got.WinRate, win},
		"median_gross": {got.MedianGrossBps, medGross}, "median_net": {got.MedianNetBps, medNet},
	} {
		if !near(pair[0], pair[1]) {
			t.Errorf("%s: %s = %v, want %v", name, field, pair[0], pair[1])
		}
	}
	if got.MedianSamplePercent != 100 {
		t.Errorf("%s: median_sample_percent = %d, want 100 (exact)", name, got.MedianSamplePercent)
	}
}

func TestSnapshotsScopeAggregatesBothSidesPerHorizon(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(nil)).data(t)
	if d.Scope != "snapshots" || d.Direction != nil || d.LabelVersion != 1 {
		t.Fatalf("scope/direction/version = %q/%v/%d", d.Scope, d.Direction, d.LabelVersion)
	}
	if len(d.Horizons) != 3 || d.Horizons[0].Horizon != "30m" || d.Horizons[1].Horizon != "60m" || d.Horizons[2].Horizon != "close" {
		t.Fatalf("horizons = %+v, want 30m, 60m, close in order", d.Horizons)
	}

	h30 := horizon(t, d, "30m")
	// LONG: rows s1,s2,s3,s5 (s4 has no ask). gross 30,-10,5,15 -> mean 10, median 10;
	// net 20,-20,-5,5 -> mean 0, median 0; wins s1,s5 -> 0.5; days 10-13 (3) and 10-14 (1).
	wantSide(t, "30m long", h30.Long, 4, 2, 10, 0, 0.5, 10, 0)
	// SHORT: all five rows. gross -30,10,-5,40,-15 -> mean 0, median -5;
	// net -42,-2,-17,28,-27 -> mean -12, median -17; wins s4 -> 0.2.
	wantSide(t, "30m short", h30.Short, 5, 2, 0, -12, 0.2, -5, -17)

	// Two days: the day-clustered bootstrap has an interval. LONG day sums of net are -5 (3 rows)
	// and 5 (1 row), so a draw is -5/3 (both 10-13), 0 (mixed) or 5 (both 10-14): the 2.5% / 97.5%
	// percentiles are exactly -5/3 and 5, whatever the stream.
	ci := h30.Long.MeanNetCI
	if ci == nil || !near(ci.Lower, -5.0/3) || !near(ci.Upper, 5) {
		t.Fatalf("30m long mean net CI = %+v, want [-5/3, 5]", ci)
	}
	if h30.Long.SEBps == nil || h30.Long.DetectableMinimumBps == nil {
		t.Fatal("standard error / detectable minimum missing with two days")
	}
	// (z(0.975) + z(0.8)) = 1.959964 + 0.841621 = 2.801585
	if !near(*h30.Long.DetectableMinimumBps/(*h30.Long.SEBps), 1.959963984540054+0.8416212335729143) {
		t.Errorf("detectable minimum / SE = %v, want z(0.975) + z(0.8)", *h30.Long.DetectableMinimumBps/(*h30.Long.SEBps))
	}

	h60 := horizon(t, d, "60m")
	// LONG: s1 gross 50 net 40; s2 gross 10 net 0 (a zero is not a win). One day only: no interval.
	wantSide(t, "60m long", h60.Long, 2, 1, 30, 20, 0.5, 30, 20)
	wantSide(t, "60m short", h60.Short, 2, 1, -30, -42, 0, -30, -42)
	if h60.Long.MeanNetCI != nil || h60.Long.SEBps != nil || h60.Long.DetectableMinimumBps != nil {
		t.Errorf("60m long has an interval from a single day: %+v", h60.Long)
	}

	hc := horizon(t, d, "close")
	wantSide(t, "close long", hc.Long, 1, 1, 100, 90, 1, 100, 90)
	wantSide(t, "close short", hc.Short, 1, 1, -100, -112, 0, -100, -112)

	if d.RecordedThrough == nil || *d.RecordedThrough != "2026-10-14" {
		t.Errorf("recorded_through = %v, want 2026-10-14", d.RecordedThrough)
	}
	if !strings.Contains(d.Notice, "研究専用") || !strings.Contains(d.Notice, "しきい値の判定") {
		t.Errorf("notice = %q", d.Notice)
	}
	if !strings.Contains(d.Definitions.Spread, "spread_bps / 2") || len(d.Definitions.Exit) != 3 || len(d.Definitions.Scope) != 3 {
		t.Errorf("definitions incomplete: %+v", d.Definitions)
	}
}

func TestCostVersusSpreadNumbers(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(nil)).data(t)
	c := horizon(t, d, "30m").CostVsSpread

	// LONG 30m: cost 10 on all four rows; spreads 8,14,20,10 -> mean 13, median 12;
	// gross - spread/2 = 26,-17,-5,10 -> mean 3.5; spread > cost(10) on 14 and 20 only -> 0.5.
	l := c.Long
	if l == nil || l.Rows != 4 || !near(l.AssumedCost.MeanBps, 10) || !near(l.AssumedCost.MedianBps, 10) || l.RowsWithSpread != 4 {
		t.Fatalf("long cost = %+v", l)
	}
	if l.Spread == nil || !near(l.Spread.MeanBps, 13) || !near(l.Spread.MedianBps, 12) {
		t.Errorf("long spread = %+v, want mean 13 median 12", l.Spread)
	}
	if l.NetAfterSpreadBps == nil || !near(*l.NetAfterSpreadBps, 3.5) {
		t.Errorf("long net_after_spread = %v, want 3.5", l.NetAfterSpreadBps)
	}
	if l.ShareSpreadAboveCost == nil || !near(*l.ShareSpreadAboveCost, 0.5) {
		t.Errorf("long share above cost = %v, want 0.5 (a spread equal to the cost is not above it)", l.ShareSpreadAboveCost)
	}

	// SHORT 30m: cost 12 on all five rows; s4 has no spread, so four rows with spread (8,14,20,10:
	// mean 13, median 12); gross - spread/2 = -34,3,-15,-20 -> mean -16.5; above 12: 14, 20 -> 0.5.
	s := c.Short
	if s == nil || s.Rows != 5 || !near(s.AssumedCost.MeanBps, 12) || s.RowsWithSpread != 4 {
		t.Fatalf("short cost = %+v", s)
	}
	if s.Spread == nil || !near(s.Spread.MeanBps, 13) || !near(s.Spread.MedianBps, 12) {
		t.Errorf("short spread = %+v", s.Spread)
	}
	if s.NetAfterSpreadBps == nil || !near(*s.NetAfterSpreadBps, -16.5) || s.ShareSpreadAboveCost == nil || !near(*s.ShareSpreadAboveCost, 0.5) {
		t.Errorf("short net_after_spread/share = %v/%v, want -16.5/0.5", s.NetAfterSpreadBps, s.ShareSpreadAboveCost)
	}
}

func TestTraderScopeUsesTheDecisionsOwnDirection(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(map[string]any{"scope": "trader"})).data(t)
	if d.Scope != "trader" {
		t.Fatalf("scope = %q", d.Scope)
	}
	h30 := horizon(t, d, "30m")
	// Decisions: LONG@s1 (gross 30, net 20), SHORT@s2 (gross 10, net -2); LONG@s4 has no LONG result,
	// the NONE decision and the others are not rows. Each side holds its own decisions only.
	wantSide(t, "trader 30m long", h30.Long, 1, 1, 30, 20, 1, 30, 20)
	wantSide(t, "trader 30m short", h30.Short, 1, 1, 10, -2, 0, 10, -2)
	if h30.Long.MeanNetCI != nil {
		t.Error("an interval from one day")
	}
	// cost in the decision scope: the decision's own side only.
	if l := h30.CostVsSpread.Long; l == nil || l.Rows != 1 || !near(l.AssumedCost.MeanBps, 10) || l.Spread == nil || !near(l.Spread.MeanBps, 8) || !near(*l.NetAfterSpreadBps, 26) {
		t.Errorf("trader long cost vs spread = %+v", l)
	}
	if s := h30.CostVsSpread.Short; s == nil || !near(s.AssumedCost.MeanBps, 12) || s.Spread == nil || !near(s.Spread.MeanBps, 14) || !near(*s.NetAfterSpreadBps, 3) {
		t.Errorf("trader short cost vs spread = %+v", s)
	}
	// 60m: LONG@s1 gross 50 net 40, SHORT@s2 gross -10 net -22. close: LONG@s1 only.
	h60 := horizon(t, d, "60m")
	wantSide(t, "trader 60m long", h60.Long, 1, 1, 50, 40, 1, 50, 40)
	wantSide(t, "trader 60m short", h60.Short, 1, 1, -10, -22, 0, -10, -22)
	hc := horizon(t, d, "close")
	wantSide(t, "trader close long", hc.Long, 1, 1, 100, 90, 1, 100, 90)
	if hc.Short != nil || hc.CostVsSpread.Short != nil {
		t.Errorf("trader close short = %+v, want null (the SHORT decision has no close row)", hc.Short)
	}

	// Coverage counts decisions: LONG/SHORT trader decisions in the period are 1,2,4,5,9 (NONE and the one
	// before the period do not count); labeled at their own side: 30m d1,d2; 60m d1,d2; close d1. Decision 9
	// sits on a day the recorder did not complete, so it is counted but not labeled.
	c := d.Coverage
	if c.Unit != "decisions" || c.InPeriod != 5 || c.Labeled["30m"] != 2 || c.Labeled["60m"] != 2 || c.Labeled["close"] != 1 {
		t.Errorf("coverage = %+v, want 4 decisions, labeled 30m 2 / 60m 2 / close 1", c)
	}
}

func TestDirectionFilterAppliesToDecisionScopes(t *testing.T) {
	path, _ := seed(t)
	srv := newTestServer(t, paperresultsmcp.Options{DBPath: path})

	d := srv.call(t, args(map[string]any{"scope": "trader", "direction": "SHORT"})).data(t)
	if d.Direction == nil || *d.Direction != "SHORT" {
		t.Fatalf("direction = %v", d.Direction)
	}
	h30 := horizon(t, d, "30m")
	if h30.Long != nil || h30.CostVsSpread.Long != nil {
		t.Errorf("a SHORT filter returned a LONG side: %+v", h30.Long)
	}
	wantSide(t, "trader short 30m", h30.Short, 1, 1, 10, -2, 0, 10, -2)
	if c := d.Coverage; c.InPeriod != 1 || c.Labeled["30m"] != 1 || c.Labeled["close"] != 0 {
		t.Errorf("SHORT coverage = %+v, want 1 decision (id 2), labeled 30m only", c)
	}

	d = srv.call(t, args(map[string]any{"scope": "trader", "direction": "LONG"})).data(t)
	h30 = horizon(t, d, "30m")
	if h30.Short != nil {
		t.Errorf("a LONG filter returned a SHORT side")
	}
	wantSide(t, "trader long 30m", h30.Long, 1, 1, 30, 20, 1, 30, 20)
	// LONG decisions of the period: 1, 4, 5, 9; labeled at 30m: only 1 (4 has no ask result, 5 has no row,
	// 9 is on an unrecorded day).
	if c := d.Coverage; c.InPeriod != 4 || c.Labeled["30m"] != 1 || c.Labeled["60m"] != 1 || c.Labeled["close"] != 1 {
		t.Errorf("LONG coverage = %+v", c)
	}
}

func TestScoutScope(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, paperresultsmcp.Options{DBPath: path}).call(t, args(map[string]any{"scope": "scout"})).data(t)
	h30 := horizon(t, d, "30m")
	// scout LONG@s5 (gross 15 net 5), scout SHORT@s4 (gross 40 net 28, no spread).
	wantSide(t, "scout 30m long", h30.Long, 1, 1, 15, 5, 1, 15, 5)
	wantSide(t, "scout 30m short", h30.Short, 1, 1, 40, 28, 1, 40, 28)
	// A decision whose entry bar has no spread_bps is in the cost but not in the spread numbers.
	s := h30.CostVsSpread.Short
	if s == nil || s.Rows != 1 || s.RowsWithSpread != 0 || s.Spread != nil || s.NetAfterSpreadBps != nil || s.ShareSpreadAboveCost != nil {
		t.Errorf("scout short cost vs spread = %+v, want cost only and null spread numbers", s)
	}
	if !near(s.AssumedCost.MeanBps, 12) {
		t.Errorf("assumed cost = %+v", s.AssumedCost)
	}
	if h60 := horizon(t, d, "60m"); h60.Long != nil || h60.Short != nil {
		t.Errorf("scout 60m = %+v / %+v, want nothing (no 60m rows on its snapshots)", h60.Long, h60.Short)
	}
	if c := d.Coverage; c.Unit != "decisions" || c.InPeriod != 2 || c.Labeled["30m"] != 2 || len(c.Labeled) != 1 {
		t.Errorf("scout coverage = %+v", c)
	}
	// The trader decisions are not mixed in.
	if d.Scope != "scout" {
		t.Errorf("scope = %q", d.Scope)
	}
}
