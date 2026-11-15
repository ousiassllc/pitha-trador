package evaltests

import (
	"math"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// With the placeholder cutoff (the default) nothing is dropped: the call says so
// (llm_cutoff.applied=false, a caveat) instead of silently evaluating days the
// model may have seen.
func TestPlaceholderCutoffDropsNothingButSaysSo(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	d := srv.call(t, nil).data(t)
	if d.LLMCutoff.Status != "placeholder" || d.LLMCutoff.Applied || d.LLMCutoff.Date == "" {
		t.Errorf("llm_cutoff = %+v", d.LLMCutoff)
	}
	if got := cohortOf(t, d, m1, v3); got.ExcludedBeforeCutoff != 0 || got.Evaluated != 6 {
		t.Errorf("cohort A = %+v", got)
	}
	flagged := false
	for _, c := range d.Caveats {
		flagged = flagged || strings.Contains(c, "仮置き")
	}
	if !flagged {
		t.Errorf("no caveat about the placeholder cutoff: %v", d.Caveats)
	}
}

// A confirmed cutoff of 2026-10-06 leaves out 10-05 (ids 1, 2, 12), for the LLM and
// for every baseline alike (they are the same rows), and counts them as such.
func TestCutoffExcludesEarlierDaysForEveryArm(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	testkit.Setting(t, w, "metalabel.llm_cutoff", `"2026-10-06"`)
	testkit.Setting(t, w, "metalabel.llm_cutoff_model", `"`+m1+`"`)
	d := srv.call(t, nil).data(t)
	if d.LLMCutoff.Status != "confirmed" || !d.LLMCutoff.Applied || d.LLMCutoff.Date != "2026-10-06" || d.LLMCutoff.Model != m1 {
		t.Fatalf("llm_cutoff = %+v", d.LLMCutoff)
	}
	a := cohortOf(t, d, m1, v3)
	if a.Decisions != 11 || a.ExcludedBeforeCutoff != 3 || a.Evaluated != 4 {
		t.Fatalf("cohort A decisions %d before cutoff %d evaluated %d, want 11/3/4", a.Decisions, a.ExcludedBeforeCutoff, a.Evaluated)
	}
	if got := reasonCount(a, evaldiag.ReasonNoBarMomentum); got != 0 { // id 12 is before the cutoff, not a missing bar
		t.Errorf("no_bar_for_momentum = %d, want 0", got)
	}
	// Rows 3, 4, 5, 6: llm -30 +30 0 -30 = -7.5; momentum +30 +30 0 0 = 15; the stored label of the same rows follows.
	wantMean(t, "llm", armOf(t, a, evaldiag.ArmLLM).All, 4, -7.5)
	wantMean(t, "momentum", armOf(t, a, evaldiag.ArmMomentum).All, 4, 15)
	wantMean(t, "none", armOf(t, a, evaldiag.ArmNone).All, 4, 0)
	if a.StoredLabel.EvaluatedRows.WithLabel != 4 || math.Abs(*a.StoredLabel.EvaluatedRows.MeanNetBps+7.5) > eps {
		t.Errorf("stored on evaluated = %+v", a.StoredLabel.EvaluatedRows)
	}
	// The count of a cohort that starts on the cutoff day is not cut at all.
	if b := cohortOf(t, d, m2, v4); b.ExcludedBeforeCutoff != 0 || b.Evaluated != 3 {
		t.Errorf("cohort B = %+v", b)
	}
	if got := armOf(t, a, evaldiag.ArmLLM).All; got.Days != 2 {
		t.Errorf("days = %d, want 2 (10-06 and 10-07)", got.Days)
	}
}

// Every arm is re-labeled with the settings in force now: a raised barrier cost
// moves the recomputed nets while the stored labels (made earlier) stay, and the
// reconciliation shows the gap.
func TestRecomputedLabelFollowsCurrentSettingsAndTheGapIsReported(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	testkit.Setting(t, w, "calibration.barrier_cost_bps", "12")
	d := srv.call(t, nil).data(t)
	if d.LabelAssumptions.BarrierCostBps != 12 || d.LabelAssumptions.LabelCostBps.Long != 12 || d.LabelAssumptions.LabelCostBps.Short != 12 {
		t.Fatalf("label assumptions = %+v", d.LabelAssumptions)
	}
	a := cohortOf(t, d, m1, v3)
	// TP +30 and SL -30 are the barrier levels; only the timeout rows move (id 5, LONG mild: +10 gross - 12 = -2).
	wantMean(t, "llm", armOf(t, a, evaldiag.ArmLLM).All, 6, -32.0/6)
	s := a.StoredLabel
	if math.Abs(*s.EvaluatedRows.MeanNetBps+5) > eps { // the stored rows were made with cost 10
		t.Errorf("stored mean = %v, want -5 (unchanged)", *s.EvaluatedRows.MeanNetBps)
	}
	if s.MaxAbsDifferenceBps == nil || math.Abs(*s.MaxAbsDifferenceBps-2) > eps || math.Abs(*s.RecomputedMeanOnLabeled+32.0/6) > eps {
		t.Errorf("reconciliation = recomputed %v, max abs diff %v, want -32/6 and 2", deref(s.RecomputedMeanOnLabeled), deref(s.MaxAbsDifferenceBps))
	}
}

// horizon_minutes changes the label's holding time and the stored label it is
// reconciled with: at 5 minutes the decisions whose data ends at +3 / whose short
// leg enters late become comparable (ids 10 and 13), and no stored label exists
// at that horizon.
func TestHorizonArgumentChangesTheLabelAndTheStoredJoin(t *testing.T) {
	srv, w := newServer(t)
	seedSmall(t, w)
	d := srv.call(t, map[string]any{"horizon_minutes": 5}).data(t)
	if d.HorizonMinutes != 5 || d.LabelAssumptions.HorizonMinutes != 5 {
		t.Fatalf("horizon = %d / %d", d.HorizonMinutes, d.LabelAssumptions.HorizonMinutes)
	}
	a := cohortOf(t, d, m1, v3)
	if a.Evaluated != 8 || reasonCount(a, evaldiag.PrefixLLM+evaldiag.KindWindowShort) != 0 || reasonCount(a, evaldiag.PrefixOpposite+evaldiag.KindWindowShort) != 0 {
		t.Fatalf("cohort A at 5 minutes: evaluated %d, reasons %+v", a.Evaluated, a.ExcludedByReason)
	}
	// ids 1-6 as before (-30 in total) plus 10 and 13 (LONG up: +30 each) = +30 over 8 rows.
	wantMean(t, "llm", armOf(t, a, evaldiag.ArmLLM).All, 8, 30.0/8)
	if a.StoredLabel.HorizonMinutes != 5 || a.StoredLabel.AllDecisions.WithLabel != 0 || a.StoredLabel.AllDecisions.MeanNetBps != nil || a.StoredLabel.MaxAbsDifferenceBps != nil {
		t.Errorf("stored label at 5 minutes = %+v", a.StoredLabel)
	}
	// The default is the Settings timeout.
	srv2 := srv.call(t, nil).data(t)
	if srv2.HorizonMinutes != 10 {
		t.Errorf("default horizon = %d", srv2.HorizonMinutes)
	}
}
