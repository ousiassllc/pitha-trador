package experimentlabeltests

import (
	"encoding/json"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment/labelcompare"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// compareData is experiment_compare's response: the trade-based Comparison
// and the label-based member.
type compareData struct {
	experiment.Comparison
	LabelBased labelcompare.Result `json:"label_based"`
}

func compareOf(t *testing.T, srv *testServer, id int64) compareData {
	t.Helper()
	var c compareData
	srv.call(t, "experiment_compare", map[string]any{"experiment_id": id}).data(t, "experiment_compare", &c)
	return c
}

func near(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

// With no closed trade at all the trade-based verdict is withheld
// (insufficient_trades), and the label-based side still answers: counts, means,
// the day-clustered interval and the days still needed.
func TestLabelBasedComparisonWorksWithZeroTrades(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, labelcompare.MinDaysFloor, -2, 4)
	for i, d := range afterDates { // trade signals: skips and their reasons
		at := d + "T01:30:00Z"
		signal(t, w, int64(100+i), at, true, "")
		signal(t, w, int64(200+i), at, false, "jev_none: confidence=0.5 regime=RANGE")
		signal(t, w, int64(300+i), at, false, "risk_engine_rejected:max_daily_trades: limit 5")
	}
	e := createExperiment(t, srv, plantedSwitch(t, w))
	c := compareOf(t, srv, e.ID)

	if c.Verdict != experiment.VerdictInsufficientTrades || c.Judged || c.Before.Performance.ClosedTradeCount != 0 || c.After.Performance.ClosedTradeCount != 0 {
		t.Fatalf("trade-based side: verdict=%s judged=%v trades=%d/%d", c.Verdict, c.Judged, c.Before.Performance.ClosedTradeCount, c.After.Performance.ClosedTradeCount)
	}
	lb := c.LabelBased
	if lb.Error != "" || lb.Caution == "" {
		t.Fatalf("label_based: error=%q caution=%q", lb.Error, lb.Caution)
	}
	wantDecisions := labelcompare.Decisions{Long: 16, Short: 8, None: 8, Total: 32}
	if lb.Before.Decisions != wantDecisions || lb.After.Decisions != wantDecisions {
		t.Errorf("decisions before=%+v after=%+v, want %+v", lb.Before.Decisions, lb.After.Decisions, wantDecisions)
	}
	if lb.Before.Days != 8 || lb.After.Days != 8 {
		t.Errorf("days = %d/%d, want 8/8", lb.Before.Days, lb.After.Days)
	}
	dl := lb.DecisionLabels
	if dl == nil || dl.HorizonMinutes != 10 {
		t.Fatalf("decision_labels = %+v", dl)
	}
	all := dl.All
	if !near(all.Before.MeanNetBps, -2) || !near(all.After.MeanNetBps, 4) || all.Before.N != 16 || all.After.N != 16 {
		t.Errorf("all: before %+v after %+v", all.Before, all.After)
	}
	if all.Before.CILow == nil || all.Before.CIHigh == nil || *all.Before.CILow >= -2 || *all.Before.CIHigh <= -2 {
		t.Errorf("before interval %+v does not bracket the mean", all.Before)
	}
	if all.Diff == nil || !near(&all.Diff.DiffBps, 6) || all.Diff.CILow == nil || *all.Diff.CILow <= 0 {
		t.Fatalf("diff = %+v, want 6 bps with an interval above 0", all.Diff)
	}
	if !near(dl.Long.After.MeanNetBps, 4) || dl.Short.After.N != 0 {
		t.Errorf("long %+v short %+v: SHORT decisions have no label", dl.Long.After, dl.Short.After)
	}

	// Research labels: 30m LONG means (base + 2): 0 before, 6 after.
	h30 := lb.ResearchLabels[0]
	if h30.Horizon != "30m" || !near(h30.Long.Before.MeanNetBps, 0) || !near(h30.Long.After.MeanNetBps, 6) || h30.Long.Diff == nil || h30.Long.After.Days != 8 {
		t.Errorf("research 30m long = %+v", h30.Long)
	}
	if h60 := lb.ResearchLabels[1]; h60.Long.After.N != 0 || h60.Long.Diff != nil {
		t.Errorf("research 60m has no rows: %+v", h60.Long)
	}

	// Skips: 8 policy + 8 risk skips against 8 passed signals; fixed codes only.
	sk := lb.After.Skips
	if sk == nil || sk.SkipCount != 16 || sk.RiskPassedCount != 8 || sk.SkipRate == nil || !near(sk.SkipRate, 16.0/24) {
		t.Fatalf("after skips = %+v", sk)
	}
	codes := []string{}
	for _, r := range sk.TopReasons {
		codes = append(codes, r.Reason)
	}
	if !slices.Equal(codes, []string{"jev_none:regime=RANGE", "risk_engine_rejected:max_daily_trades"}) {
		t.Errorf("top reasons = %v", codes)
	}
	if lb.Before.Skips == nil || lb.Before.Skips.SkipCount != 0 {
		t.Errorf("before skips = %+v", lb.Before.Skips)
	}

	// Days needed: the difference is large against the daily spread, so the
	// floor (8 days) is what binds, and 8 are observed.
	dn := lb.DaysNeeded
	if dn == nil || dn.ObservedPerSide != 8 || dn.MinDaysSource != "measured_v1" || dn.MinDays != labelcompare.MinDaysFloor || dn.DaysToMin != 0 ||
		dn.NeededPerSide == nil || *dn.NeededPerSide != labelcompare.MinDaysFloor || dn.RemainingDays == nil || *dn.RemainingDays != 0 {
		t.Errorf("days_needed = %+v", dn)
	}
	if lb.LabelVerdict != labelcompare.VerdictImproved || !lb.LabelJudged {
		t.Errorf("label_verdict = %s judged=%v", lb.LabelVerdict, lb.LabelJudged)
	}
}

// Fewer days than the floor: the figures are there, the judgement is withheld,
// and the days still to wait are reported.
func TestLabelBasedWithSparseDaysIsInsufficientDays(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, 2, -2, 4)
	e := createExperiment(t, srv, plantedSwitch(t, w))
	lb := compareOf(t, srv, e.ID).LabelBased
	if lb.LabelVerdict != labelcompare.VerdictInsufficientDays || lb.LabelJudged {
		t.Fatalf("label_verdict = %s judged=%v", lb.LabelVerdict, lb.LabelJudged)
	}
	dn := lb.DaysNeeded
	if dn == nil || dn.ObservedPerSide != 2 || dn.DaysToMin != labelcompare.MinDaysFloor-2 || dn.RemainingDays == nil || *dn.RemainingDays != labelcompare.MinDaysFloor-2 {
		t.Errorf("days_needed = %+v, want %d days to the floor", dn, labelcompare.MinDaysFloor-2)
	}
	if lb.DecisionLabels.All.Diff == nil || lb.DecisionLabels.All.Before.Days != 2 {
		t.Errorf("figures must still be returned: %+v", lb.DecisionLabels.All)
	}
}

// The floor is exactly MinDaysFloor (8) days: one day short is still
// insufficient_days (with min_days, min_days_source and days_to_min in the
// response), eight is judged. The intervals say how they were made.
func TestLabelBasedFloorIsEightDaysAndNamesItsSource(t *testing.T) {
	if labelcompare.MinDaysFloor != 8 {
		t.Fatalf("MinDaysFloor = %d, want 8 (docs §2.4 derive it as the smallest n >= 2*1.96^2)", labelcompare.MinDaysFloor)
	}
	for days, want := range map[int]string{7: labelcompare.VerdictInsufficientDays, 8: labelcompare.VerdictImproved} {
		srv, w := newServer(t)
		plantLabels(t, w, days, -2, 4)
		e := createExperiment(t, srv, plantedSwitch(t, w))
		lb := compareOf(t, srv, e.ID).LabelBased
		dn := lb.DaysNeeded
		if lb.LabelVerdict != want || dn == nil || dn.MinDays != 8 || dn.MinDaysSource != "measured_v1" || dn.DaysToMin != 8-days || dn.ObservedPerSide != days {
			t.Errorf("%d days: label_verdict = %s (want %s), days_needed = %+v", days, lb.LabelVerdict, want, dn)
		}
		if lb.IntervalMethod != "newey_west_bartlett" || lb.IntervalLagDays != 1 {
			t.Errorf("%d days: interval_method = %q lag = %d", days, lb.IntervalMethod, lb.IntervalLagDays)
		}
	}
}

// The lag of the correction is the longest holding time rounded up to days. A
// stored label holds for at most the barrier timeout, a research label until
// the session close, both within the day they start on: one day.
func TestNeweyWestLagIsTheLongestHoldInDays(t *testing.T) {
	longest := time.Duration(barriersettings.MaxTimeoutMinutes) * time.Minute
	if want := int((longest + 24*time.Hour - 1) / (24 * time.Hour)); labelcompare.NeweyWestLagDays != want {
		t.Errorf("NeweyWestLagDays = %d, want %d (ceil of the %v barrier timeout in days)", labelcompare.NeweyWestLagDays, want, longest)
	}
}

// A difference that is small against the day-to-day spread needs more days than
// the floor, and remaining_days says how many.
func TestLabelBasedRemainingDaysFollowTheVariance(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, labelcompare.MinDaysFloor, 0, 0.15) // a 0.15 bps difference against jitter of +-0.4
	e := createExperiment(t, srv, plantedSwitch(t, w))
	lb := compareOf(t, srv, e.ID).LabelBased
	dn := lb.DaysNeeded
	if dn == nil || dn.NeededPerSide == nil || dn.RemainingDays == nil || *dn.NeededPerSide <= labelcompare.MinDaysFloor || *dn.RemainingDays != *dn.NeededPerSide-labelcompare.MinDaysFloor {
		t.Fatalf("days_needed = %+v, want more than the floor", dn)
	}
	if lb.LabelVerdict != labelcompare.VerdictInconclusive {
		t.Errorf("label_verdict = %s, want inconclusive", lb.LabelVerdict)
	}
}

// A confounded experiment is not judged by labels either (the same state the
// trade-based verdict uses), and the figures are still returned.
func TestConfoundedExperimentHasNoLabelVerdict(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, labelcompare.MinDaysFloor, -2, 4)
	e := createExperiment(t, srv, plantedSwitch(t, w))
	auditRow(t, w, "2026-10-13T10:00:00Z", "set", "risk.max_daily_trades", "3", "4")
	c := compareOf(t, srv, e.ID)
	if !c.Confounded || c.Verdict != experiment.VerdictConfounded {
		t.Fatalf("trade-based: confounded=%v verdict=%s", c.Confounded, c.Verdict)
	}
	lb := c.LabelBased
	if lb.LabelVerdict != labelcompare.VerdictConfounded || lb.LabelJudged || lb.DecisionLabels == nil || lb.DecisionLabels.All.Diff == nil {
		t.Errorf("label_based = verdict %s judged %v figures %+v", lb.LabelVerdict, lb.LabelJudged, lb.DecisionLabels)
	}
}

// An experiment whose switch is not on record is not judged by labels.
func TestUnconfirmedSwitchHasNoLabelVerdict(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, labelcompare.MinDaysFloor, -2, 4)
	e := createExperiment(t, srv, auditRow(t, w, startedAt, "set", key, "7", "8")) // not the declared values
	lb := compareOf(t, srv, e.ID).LabelBased
	if lb.LabelVerdict != labelcompare.VerdictSwitchUnconfirmed || lb.LabelJudged {
		t.Errorf("label_verdict = %s judged=%v", lb.LabelVerdict, lb.LabelJudged)
	}
}

// No decision and no label at all: the label-based side is "insufficient_days"
// with empty figures, and the call still succeeds.
func TestLabelBasedWithNoDataAtAll(t *testing.T) {
	srv, w := newServer(t)
	e := createExperiment(t, srv, plantedSwitch(t, w))
	lb := compareOf(t, srv, e.ID).LabelBased
	if lb.Error != "" || lb.LabelVerdict != labelcompare.VerdictInsufficientDays || lb.DaysNeeded == nil || lb.DaysNeeded.RemainingDays != nil || lb.DaysNeeded.Reason == "" {
		t.Errorf("label_based = %+v / %+v", lb, lb.DaysNeeded)
	}
}

// The trade-based members are exactly what experiment.Compare gives: the
// response minus label_based equals the Comparison marshalled directly, and the
// member names are the documented ones.
func TestTradeBasedFieldsAreUnchangedByTheLabelMember(t *testing.T) {
	srv, w := newServer(t)
	plantLabels(t, w, labelcompare.MinDaysFloor, -2, 4)
	for _, at := range []string{"2026-10-05T01:00:00Z", "2026-10-06T01:00:00Z", "2026-10-13T01:00:00Z", "2026-10-14T01:00:00Z"} {
		pnl := 1000.0
		if at > startedAt {
			pnl = 2500
		}
		testkit.Position(t, w, at, &pnl, 1000, 100, "take_profit", 99, nil)
	}
	e := createExperiment(t, srv, plantedSwitch(t, w))

	got := srv.call(t, "experiment_compare", map[string]any{"experiment_id": e.ID}).members(t, "experiment_compare")
	if _, ok := got["label_based"]; !ok {
		t.Fatal("label_based is missing")
	}
	delete(got, "label_based")
	wantKeys := []string{"after", "before", "confounded", "confounders", "diff", "experiment", "judged", "min_trades", "min_trades_source", "per_trade_pnl", "switch_check", "verdict"}
	if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, wantKeys) {
		t.Fatalf("trade-based members = %v, want %v", keys, wantKeys)
	}

	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	exp, err := experiment.Get(t.Context(), db, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := experiment.Compare(t.Context(), db, exp, srvNow(), 0, 30_000_000, true)
	if err != nil {
		t.Fatal(err)
	}
	var wantMembers map[string]json.RawMessage
	b, _ := json.Marshal(direct)
	if err := json.Unmarshal(b, &wantMembers); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonical(t, got), canonical(t, wantMembers)) {
		t.Errorf("trade-based members differ from experiment.Compare:\n got  %s\n want %s", canonical(t, got), canonical(t, wantMembers))
	}
	// And the fixture's values are the trade-based ones (the seeded Paper trades).
	var c experiment.Comparison
	b, _ = json.Marshal(got)
	_ = json.Unmarshal(b, &c)
	if c.Verdict != experiment.VerdictInsufficientTrades || c.Judged || c.MinTrades != experiment.MinTradesFloor ||
		c.After.Performance.ClosedTradeCount != 2 || c.Before.Performance.ClosedTradeCount != 2 {
		t.Errorf("verdict=%s judged=%v min_trades=%d trades=%d/%d", c.Verdict, c.Judged, c.MinTrades, c.Before.Performance.ClosedTradeCount, c.After.Performance.ClosedTradeCount)
	}
}

func canonical(t *testing.T, m map[string]json.RawMessage) string {
	t.Helper()
	out := map[string]any{}
	for k, v := range m {
		var x any
		if err := json.Unmarshal(v, &x); err != nil {
			t.Fatal(err)
		}
		out[k] = x
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
