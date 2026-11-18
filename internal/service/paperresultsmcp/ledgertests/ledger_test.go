package ledgertests

import (
	"math"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
)

const (
	idShadow   = "shadow:trader-v9"
	idTrial    = "trial:arm-a"
	idResearch = "research_model:ridge:30m"
	idRandom   = "baseline:random"
)

var four = []string{idShadow, idTrial, idResearch, idRandom}

// Under common_all every candidate's values are on the very same rows: the 5 decisions (ids 1, 2, 3, 6, 7)
// the live reference and all four candidates answered. The means and the differences are hand-computed
// (the fixture comment), the p-values are those of the same differences summarized by evaldiag, and the
// Holm-adjusted values are stats.Holm's over them.
func TestCommonAllUsesTheSameRowsAndHandComputedNumbers(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	l := srv.call(t, map[string]any{"candidates": four}).ledger(t)

	if l.Reference.Rows != 8 || l.Reference.MeanNetBps == nil || !near(l.Reference.MeanNetBps, 30.0/8) || l.Reference.CommonRows == nil || *l.Reference.CommonRows != 5 {
		t.Fatalf("reference = %+v", l.Reference)
	}
	if l.Exclusions.Evaluated != 8 || l.Exclusions.NoLabel != 0 || l.Alignment != ledgerdiag.AlignCommonAll || l.Horizon != ledgerdiag.Horizon10m {
		t.Fatalf("exclusions %+v alignment %s horizon %s", l.Exclusions, l.Alignment, l.Horizon)
	}
	wantMean := map[string]float64{idShadow: 18, idTrial: -4, idResearch: -6, idRandom: -2}
	// candidate net minus live net per common row (ids 1, 2, 3, 6, 7; days 5, 5, 6, 7, 8)
	diffs := map[string][]float64{
		idShadow:   {0, 30, 60, 0, 0},
		idTrial:    {0, 0, 60, -20, -60},
		idResearch: {-60, 0, 30, 0, 0},
		idRandom:   {-30, 30, 30, -10, -30},
	}
	days := []string{"2026-10-05", "2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08"}
	var ps []float64
	var ids []string
	for _, id := range four {
		e := entryOf(t, l, id)
		if e.NCommon == nil || *e.NCommon != 5 || !near(e.MissingRate, 0.375) {
			t.Errorf("%s: n_common=%v missing_rate=%v, want 5 and 0.375", id, e.NCommon, deref(e.MissingRate))
		}
		if !near(e.MeanNetBps, wantMean[id]) || !near(e.LiveMeanOnCommonRows, 0) || e.Days != 4 || e.CI == nil {
			t.Errorf("%s: mean=%v live=%v days=%d ci=%v, want mean %v live 0 days 4", id, deref(e.MeanNetBps), deref(e.LiveMeanOnCommonRows), e.Days, e.CI, wantMean[id])
		}
		d := e.DiffVsLive
		if d == nil || d.N != 5 || !near(d.MeanBps, wantMean[id]) || d.PValue == nil {
			t.Fatalf("%s: diff = %+v", id, d)
		}
		pts := make([]evaldiag.Point, len(days))
		for i := range days {
			pts[i] = evaldiag.Point{Day: days[i], Value: diffs[id][i]}
		}
		want := evaldiag.Summarize(pts, false)
		if !near(d.PValue, *want.P) || d.CI == nil || !near(&d.CI.Lower, want.CI.Lower) || !near(&d.CI.Upper, want.CI.Upper) || !near(d.DetectableMinimumBps, *want.DetectableMinimumEdgeBps) {
			t.Errorf("%s: diff p/ci/detectable differ from the same series summarized: %+v vs %+v", id, d, want)
		}
		ps, ids = append(ps, *want.P), append(ids, id)
	}
	adj := stats.Holm(ps)
	for i, id := range ids {
		d := entryOf(t, l, id).DiffVsLive
		if d.HolmAdjustedP == nil || math.Abs(*d.HolmAdjustedP-adj[i]) > 1e-12 || d.FamilySize != 4 {
			t.Errorf("%s: holm = %v family %d, want %v family 4", id, deref(d.HolmAdjustedP), d.FamilySize, adj[i])
		}
	}
	if l.Statistics.FamilySize != 4 {
		t.Errorf("family size = %d", l.Statistics.FamilySize)
	}
}

// Pairwise compares each candidate with the live reference on the rows they share: the shadow on all 8,
// the trial on 5 (failed, refused and absent answers are missing), the research model on 7.
func TestPairwiseUsesEachCandidatesOwnCommonRows(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	l := srv.call(t, map[string]any{"candidates": four, "alignment": "pairwise"}).ledger(t)
	type want struct {
		n              int
		mean, liveMean float64
		missing        float64
		absent, failed int
		outsideCommon  int
	}
	for id, w := range map[string]want{
		idShadow:   {8, 15, 3.75, 0, 0, 0, 0},
		idTrial:    {5, -4, 0, 0.375, 1, 2, 0},
		idResearch: {7, 0, 30.0 / 7, 0.125, 1, 0, 0},
		idRandom:   {8, -3.75, 3.75, 0, 0, 0, 0},
	} {
		e := entryOf(t, l, id)
		if e.NCommon == nil || *e.NCommon != w.n || !near(e.MeanNetBps, w.mean) || !near(e.LiveMeanOnCommonRows, w.liveMean) || !near(e.MissingRate, w.missing) {
			t.Errorf("%s: n=%v mean=%v live=%v missing=%v, want %+v", id, e.NCommon, deref(e.MeanNetBps), deref(e.LiveMeanOnCommonRows), deref(e.MissingRate), w)
		}
		if e.Missing.Absent != w.absent || e.Missing.Failed != w.failed || e.Missing.OutsideCommon != w.outsideCommon {
			t.Errorf("%s: missing = %+v, want %+v", id, e.Missing, w)
		}
		if d := e.DiffVsLive; d == nil || d.N != w.n || !near(d.MeanBps, w.mean-w.liveMean) {
			t.Errorf("%s: diff = %+v", id, d)
		}
	}
	if l.Reference.CommonRows != nil {
		t.Errorf("pairwise has no single common row set: %v", *l.Reference.CommonRows)
	}
}

// Failed, refused and absent trial answers are missing rows, so they raise the missing rate of the trial
// arm (pairwise) and of every candidate (common_all, where the rows leave the common set).
func TestMissingTrialAnswersRaiseTheMissingRate(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	shadowOnly := srv.call(t, map[string]any{"candidates": []string{idShadow}}).ledger(t)
	if e := entryOf(t, shadowOnly, idShadow); !near(e.MissingRate, 0) || *e.NCommon != 8 {
		t.Fatalf("shadow alone: %+v", e)
	}
	both := srv.call(t, map[string]any{"candidates": []string{idShadow, idTrial}}).ledger(t)
	shadow, tr := entryOf(t, both, idShadow), entryOf(t, both, idTrial)
	if !near(shadow.MissingRate, 0.375) || !near(tr.MissingRate, 0.375) || shadow.Missing.OutsideCommon != 3 || tr.Missing.Failed != 2 || tr.Missing.Absent != 1 || tr.NAnswered != 5 {
		t.Errorf("shadow %+v / trial %+v", shadow, tr)
	}
}

// The AI cost is reported apart from the net bps. A trial row of unknown cost has no amount: it is
// neither added as 0 nor hidden (complete=false, no per-row cost, no bps conversion); counted
// cost of known rows is summed by source. The bps conversion needs usd_jpy and notional_jpy together.
func TestAICostIsSeparateAndUnknownIsNeverZero(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	seedCandidates(t, w, seedBase(t, w))
	args := map[string]any{"candidates": []string{idShadow, idTrial}}
	l := srv.call(t, args).ledger(t)

	// reference: 8 rows, row 8 has no recorded request_cost
	if c := l.Reference.AICost; c.Rows != 8 || c.Complete || c.RowsWithoutAmount != 1 || !near(c.USDTotal, 0.028) || c.USDPerRow != nil {
		t.Errorf("reference cost = %+v", c)
	}
	tr := entryOf(t, l, idTrial)
	c := tr.AICost
	if c.Rows != 5 || c.Complete || c.RowsWithoutAmount != 1 || !near(c.USDTotal, 0.08) || c.USDPerRow != nil {
		t.Fatalf("trial cost = %+v", c)
	}
	sources := map[string]ledgerdiag.CostSource{}
	for _, s := range c.BySource {
		sources[s.Source] = s
	}
	if s := sources["reported"]; s.Rows != 3 || !near(s.USD, 0.05) {
		t.Errorf("reported = %+v", s)
	}
	if s := sources["list_price_estimate"]; s.Rows != 1 || !near(s.USD, 0.03) {
		t.Errorf("list_price_estimate = %+v", s)
	}
	if s := sources["unknown"]; s.Rows != 1 || s.USD != nil {
		t.Errorf("unknown must carry no amount, got %+v", s)
	}
	sh := entryOf(t, l, idShadow)
	if c := sh.AICost; c.Rows != 5 || !c.Complete || !near(c.USDTotal, 0.05) || !near(c.USDPerRow, 0.01) {
		t.Errorf("shadow cost = %+v", c)
	}
	var estimate ledgerdiag.CostSource
	for _, s := range sh.AICost.BySource {
		if s.Source == ledgerdiag.CostUsageEstimate {
			estimate = s
		}
	}
	if estimate.Rows != 1 || !near(estimate.USD, 0.01) {
		t.Errorf("the estimated shadow row is not flagged: %+v", sh.AICost.BySource)
	}
	// without both conversion arguments nothing is converted
	for _, a := range []map[string]any{args, {"candidates": args["candidates"], "usd_jpy": 150.0}, {"candidates": args["candidates"], "notional_jpy": 1_000_000.0}} {
		got := entryOf(t, srv.call(t, a).ledger(t), idShadow).Combined
		if got.CostBpsPerRow != nil || got.MeanNetAfterAICostBps != nil || got.Reason != "usd_jpy_and_notional_jpy_required" {
			t.Errorf("combined without both arguments = %+v", got)
		}
	}
	args["usd_jpy"], args["notional_jpy"] = 150.0, 1_000_000.0
	l = srv.call(t, args).ledger(t)
	// 0.01 USD x 150 / 1e6 x 1e4 = 0.015 bps; the live reference's 0.004 USD is 0.006 bps
	got := entryOf(t, l, idShadow).Combined
	if !near(got.CostBpsPerRow, 0.015) || !near(got.MeanNetAfterAICostBps, 18-0.015) || !near(got.LiveCostBpsPerRow, 0.006) || !near(got.DiffAfterAICostBps, 18-(0.015-0.006)) {
		t.Errorf("shadow combined = %+v", got)
	}
	// the trial's cost is unknown on one row: no number, a reason
	if got := entryOf(t, l, idTrial).Combined; got.CostBpsPerRow != nil || got.MeanNetAfterAICostBps != nil || got.Reason != "cost_unknown" {
		t.Errorf("trial combined = %+v", got)
	}
}

// distinct_model_ids tell a model change: live from jev_decisions.model_id, shadow from model_id,
// trial from served_model. Another model under the live version is flagged on the reference, and the
// model_id argument restricts the reference (the other model becomes a live_cohort of its own).
func TestDistinctModelIDsAndTheLiveModelFilter(t *testing.T) {
	t.Parallel()
	srv, w := newServer(t, "")
	decs := seedBase(t, w)
	seedCandidates(t, w, decs)
	extra := []dec{
		{id: 20, day: 8, slot: 2, dir: "LONG", model: m2, version: live, scenario: "up", cost: 0.004, r5: 1.0},
		{id: 21, day: 5, slot: 2, dir: "LONG", model: m2, version: "trader-v2", scenario: "up", cost: 0.004, r5: 1.0},
		{id: 22, day: 6, slot: 2, dir: "SHORT", model: m2, version: "trader-v2", scenario: "up", cost: 0.004, r5: 1.0},
	}
	insertDecisions(t, w, extra)

	l := srv.call(t, nil).ledger(t)
	if !l.Reference.MultipleModelID || !slices.Equal(l.Reference.ModelIDs, []string{m1, m2}) || l.Reference.Rows != 9 {
		t.Errorf("reference = %+v", l.Reference)
	}
	if e := entryOf(t, l, idShadow); !slices.Equal(e.DistinctModelIDs, []string{"shadow-m"}) {
		t.Errorf("shadow models = %v", e.DistinctModelIDs)
	}
	if e := entryOf(t, l, idTrial); !slices.Equal(e.DistinctModelIDs, []string{"served-1"}) {
		t.Errorf("trial models = %v", e.DistinctModelIDs)
	}
	// another cohort: its own statistics only, the difference is null because it judged other decisions
	c := entryOf(t, l, "live_cohort:model-2/trader-v2")
	if c.NAnswered != 2 || !near(c.MeanNetBps, 0) || c.DiffVsLive != nil || c.DiffReason != "different_decisions" || c.NCommon != nil || c.MissingRate != nil {
		t.Errorf("cohort = %+v", c)
	}

	f := srv.call(t, map[string]any{"model_id": m1}).ledger(t)
	if f.Reference.MultipleModelID || !slices.Equal(f.Reference.ModelIDs, []string{m1}) || f.Reference.Rows != 8 || f.Reference.ModelIDFilter != m1 {
		t.Errorf("filtered reference = %+v", f.Reference)
	}
	if e := entryOf(t, f, "live_cohort:model-2/"+live); e.NAnswered != 1 || e.DiffVsLive != nil {
		t.Errorf("the other model of the live version = %+v", e)
	}
}
