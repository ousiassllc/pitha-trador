package counterfactual_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/counterlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration/counterfactual"
)

// The counterfactual label is the one the live labeling computes: the same
// decision and bars give the same barrier, net bps, entry reference and
// excursions as HandleJob's calibration_outcomes row.
func TestRecorder_LabelEqualsTheLiveLabeling(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	d := e.decision(t, base, domain.JevDirectionLong)
	e.bars(t, base, tpBars)

	if n, err := e.recorder.Run(context.Background()); err != nil || n != 1 {
		t.Fatalf("Run = (%d, %v), want 1", n, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM calibration_outcomes`); n != 0 {
		t.Fatalf("the recorder wrote %d calibration_outcomes rows, want 0", n)
	}

	payload, _ := json.Marshal(calrepo.OutcomeLabelJobPayload{JevDecisionID: d.ID, HorizonMinutes: 5})
	if err := e.labeler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	var liveHit string
	var liveNet, liveRef, liveFuture, liveMAE, liveMFE float64
	if err := e.db.QueryRow(`SELECT barrier_hit, net_return_bps, entry_ref_price, future_return, max_adverse_excursion, max_favorable_excursion FROM calibration_outcomes WHERE jev_decision_id = ?`, d.ID).
		Scan(&liveHit, &liveNet, &liveRef, &liveFuture, &liveMAE, &liveMFE); err != nil {
		t.Fatalf("read live label: %v", err)
	}
	var hit, cutoff string
	var net, ref, future, mae, mfe float64
	var version, horizon int
	if err := e.db.QueryRow(`SELECT barrier_hit, net_return_bps, entry_ref_price, future_return, max_adverse_excursion, max_favorable_excursion, label_version, horizon_minutes, llm_cutoff FROM counterfactual_labels WHERE jev_decision_id = ?`, d.ID).
		Scan(&hit, &net, &ref, &future, &mae, &mfe, &version, &horizon, &cutoff); err != nil {
		t.Fatalf("read counterfactual label: %v", err)
	}
	if hit != liveHit || !almostEqual(net, liveNet) || !almostEqual(ref, liveRef) || !almostEqual(future, liveFuture) || !almostEqual(mae, liveMAE) || !almostEqual(mfe, liveMFE) {
		t.Fatalf("counterfactual %s/%v/%v/%v/%v/%v differs from the live label %s/%v/%v/%v/%v/%v", hit, net, ref, future, mae, mfe, liveHit, liveNet, liveRef, liveFuture, liveMAE, liveMFE)
	}
	if hit != domain.BarrierHitTakeProfit || !almostEqual(net, 30) || version != domain.CalibrationLabelVersionTripleBarrier || horizon != 5 || cutoff != "2026-09-20" {
		t.Fatalf("label = %s/%v v%d %dm cutoff %s, want a 5-minute tp of +30 bps, version 2", hit, net, version, horizon, cutoff)
	}
}

// Skipped LONG/SHORT candidates the live labeling left behind get a label with
// their reject reason; entered ones, NONE decisions and already labeled ones do
// not, a second run adds nothing, and an existing calibration_outcomes row is
// never touched.
func TestRecorder_LabelsOnlySkippedLongShortWithoutALabel(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	skipped := e.decision(t, base, domain.JevDirectionLong)
	e.signal(t, skipped.ID, base, false, "entry_quality_below_threshold")
	// No signal at all; a SHORT on rising bars is a stop-loss.
	noSignal := e.decision(t, base.Add(time.Hour), domain.JevDirectionShort)
	entered := e.decision(t, base.Add(2*time.Hour), domain.JevDirectionLong)
	e.signal(t, entered.ID, base.Add(2*time.Hour), true, "")
	none := e.decision(t, base.Add(3*time.Hour), domain.JevDirectionNone)
	e.signal(t, none.ID, base.Add(3*time.Hour), false, "jev_none")
	labeled := e.decision(t, base.Add(4*time.Hour), domain.JevDirectionLong)
	e.signal(t, labeled.ID, base.Add(4*time.Hour), false, "not_lendable")
	for _, d := range []domain.JevDecision{skipped, noSignal, entered, none, labeled} {
		e.bars(t, d.Timestamp, tpBars)
	}
	payload, _ := json.Marshal(calrepo.OutcomeLabelJobPayload{JevDecisionID: labeled.ID, HorizonMinutes: 5})
	if err := e.labeler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	var beforeNet float64
	if err := e.db.QueryRow(`SELECT net_return_bps FROM calibration_outcomes WHERE jev_decision_id = ?`, labeled.ID).Scan(&beforeNet); err != nil {
		t.Fatal(err)
	}

	n, err := e.recorder.Run(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("Run = (%d, %v), want 2 labels (the skipped LONG and the unsignalled SHORT)", n, err)
	}
	var reason string
	if err := e.db.QueryRow(`SELECT reject_reason FROM counterfactual_labels WHERE jev_decision_id = ?`, skipped.ID).Scan(&reason); err != nil || reason != "entry_quality_below_threshold" {
		t.Fatalf("reject_reason = %q, %v, want entry_quality_below_threshold", reason, err)
	}
	var shortHit string
	if err := e.db.QueryRow(`SELECT barrier_hit FROM counterfactual_labels WHERE jev_decision_id = ?`, noSignal.ID).Scan(&shortHit); err != nil || shortHit != domain.BarrierHitStopLoss {
		t.Fatalf("SHORT label = %q, %v, want sl (the price rose)", shortHit, err)
	}
	for _, d := range []domain.JevDecision{entered, none, labeled} {
		if c := e.count(t, `SELECT COUNT(*) FROM counterfactual_labels WHERE jev_decision_id = ?`, d.ID); c != 0 {
			t.Errorf("decision %d was labeled counterfactually, want none", d.ID)
		}
	}
	var afterNet float64
	if err := e.db.QueryRow(`SELECT net_return_bps FROM calibration_outcomes WHERE jev_decision_id = ?`, labeled.ID).Scan(&afterNet); err != nil || afterNet != beforeNet {
		t.Errorf("the existing calibration_outcomes number changed: %v -> %v (%v)", beforeNet, afterNet, err)
	}
	if c := e.count(t, `SELECT COUNT(*) FROM calibration_outcomes`); c != 1 {
		t.Errorf("calibration_outcomes rows = %d, want the one the live labeling wrote", c)
	}
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 0 {
		t.Fatalf("second Run = (%d, %v), want 0 (already labeled)", n, err)
	}
}

// Nothing is recorded while the LLM cutoff is the provisional placeholder, and a
// decision before the confirmed cutoff is never labeled (no label overlaps the
// LLM's training period).
func TestRecorder_RespectsTheLLMCutoff(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	e.decision(t, base, domain.JevDirectionLong)
	e.bars(t, base, tpBars)
	labels := func() int { return e.count(t, `SELECT COUNT(*) FROM counterfactual_labels`) }

	e.settings.LLMCutoff, e.settings.LLMCutoffModel = metalabelsettings.DefaultLLMCutoff, ""
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 0 || labels() != 0 {
		t.Fatalf("placeholder cutoff: Run = (%d, %v), labels %d, want nothing recorded", n, err, labels())
	}
	e.settings.LLMCutoff, e.settings.LLMCutoffModel = "2026-09-28", "m" // after the decision
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 0 || labels() != 0 {
		t.Fatalf("decision before the cutoff: Run = (%d, %v), labels %d, want nothing recorded", n, err, labels())
	}
	e.settings.LLMCutoff = "2026-09-27" // the cutoff day itself is in
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 1 {
		t.Fatalf("decision on the cutoff day: Run = (%d, %v), want 1", n, err)
	}
}

// A decision the per-minute labeling can still enqueue (inside its 24 hour retry
// window) is left to it: the recorder never races it into a double label.
func TestRecorder_LeavesRecentDecisionsToTheLiveLabeling(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	e.decision(t, base, domain.JevDirectionLong)
	e.bars(t, base, tpBars)

	e.now = base.Add(24 * time.Hour) // still inside the retry window
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 0 {
		t.Fatalf("Run inside the retry window = (%d, %v), want 0", n, err)
	}
	e.now = base.Add(26 * time.Hour)
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 1 {
		t.Fatalf("Run after the retry window = (%d, %v), want 1", n, err)
	}
}

// A skipped candidate whose window never reaches the horizon gets no label (a
// shortened window is never recorded) and is marked once, so it is not tried
// every night.
func TestRecorder_UnlabelableCandidateIsMarkedAndNotRetried(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	d := e.decision(t, base, domain.JevDirectionLong)
	e.bars(t, base, map[time.Duration]bar{0: {price: 999, bid: 998, ask: 1000}})

	if n, err := e.recorder.Run(context.Background()); err != nil || n != 0 {
		t.Fatalf("Run = (%d, %v), want nothing recorded", n, err)
	}
	if c := e.count(t, `SELECT COUNT(*) FROM counterfactual_labels`); c != 0 {
		t.Fatalf("counterfactual labels = %d, want none for a short window", c)
	}
	if c := e.count(t, `SELECT COUNT(*) FROM calibration_label_skips WHERE jev_decision_id = ? AND horizon_minutes = 5`, d.ID); c != 1 {
		t.Fatalf("label skips = %d, want the pair marked", c)
	}
	targets, err := e.store.PendingTargets(context.Background(), 5, time.Time{}, e.now, 10)
	if err != nil || len(targets) != 0 {
		t.Fatalf("PendingTargets = %v, %v, want none after the mark", targets, err)
	}
}

// A NONE decision reaching the labeling step is refused, never given an imputed
// direction, and marks nothing.
func TestRecorder_NoneIsNeverDirectionImputed(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	d := e.decision(t, base, domain.JevDirectionNone)
	e.bars(t, base, tpBars)

	failing := fixedTargets{target: counterlabel.Target{JevDecisionID: d.ID}, Store: e.store}
	r := counterfactual.New(failing, e.labeler, e.outcomes,
		func(context.Context) (metalabelsettings.Settings, error) { return e.settings, nil },
		func(context.Context) ([]int, error) { return []int{5}, nil },
	).WithClock(func() time.Time { return e.now })
	if _, err := r.Run(context.Background()); !errors.Is(err, counterfactual.ErrNoDirection) {
		t.Fatalf("Run with a NONE target = %v, want ErrNoDirection", err)
	}
	if c := e.count(t, `SELECT COUNT(*) FROM counterfactual_labels`) + e.count(t, `SELECT COUNT(*) FROM calibration_label_skips`); c != 0 {
		t.Fatalf("a NONE decision wrote %d rows, want 0", c)
	}
}

// fixedTargets hands the recorder one fixed target (what the selection query
// never does for a NONE decision), delegating Insert.
type fixedTargets struct {
	counterfactual.Store
	target counterlabel.Target
}

func (f fixedTargets) PendingTargets(context.Context, int, time.Time, time.Time, int) ([]counterlabel.Target, error) {
	return []counterlabel.Target{f.target}, nil
}

// Risk Engine rejections, Policy guard skips and decisions with no signal at all
// are recorded in their own skip_kind, not as one "skipped".
func TestRecorder_SeparatesRiskRejectionsFromGuardSkips(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	risk := e.decision(t, base, domain.JevDirectionLong)
	e.signal(t, risk.ID, base, false, "risk_engine_rejected: bucket_budget_exceeded: used 1.2M of 1.0M")
	guard := e.decision(t, base.Add(time.Hour), domain.JevDirectionLong)
	e.signal(t, guard.ID, base.Add(time.Hour), false, "entry_quality_below_threshold")
	none := e.decision(t, base.Add(2*time.Hour), domain.JevDirectionShort)
	for _, d := range []domain.JevDecision{risk, guard, none} {
		e.bars(t, d.Timestamp, tpBars)
	}

	if n, err := e.recorder.Run(context.Background()); err != nil || n != 3 {
		t.Fatalf("Run = (%d, %v), want 3", n, err)
	}
	want := map[int64][2]string{
		risk.ID:  {"risk_rejected", "risk_engine_rejected: bucket_budget_exceeded: used 1.2M of 1.0M"},
		guard.ID: {"guard_skipped", "entry_quality_below_threshold"},
		none.ID:  {"no_signal", ""},
	}
	for id, w := range want {
		var kind, reason string
		if err := e.db.QueryRow(`SELECT skip_kind, reject_reason FROM counterfactual_labels WHERE jev_decision_id = ?`, id).Scan(&kind, &reason); err != nil {
			t.Fatal(err)
		}
		if kind != w[0] || reason != w[1] {
			t.Errorf("decision %d: skip_kind %q reject_reason %q, want %q / %q", id, kind, reason, w[0], w[1])
		}
	}
}

// With a cutoff that has a date but no recorded Jev model the recorder keeps
// recording, and marks the labels; a confirmed cutoff leaves them unmarked.
func TestRecorder_MarksLabelsRecordedUnderAnUnverifiedCutoff(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	e.decision(t, base, domain.JevDirectionLong)
	e.decision(t, base.Add(time.Hour), domain.JevDirectionLong)
	e.bars(t, base, tpBars)
	e.bars(t, base.Add(time.Hour), tpBars)
	unverified := func() int {
		return e.count(t, `SELECT COUNT(*) FROM counterfactual_labels WHERE cutoff_unverified = 1`)
	}

	e.settings.LLMCutoffModel = "" // a date, no model: unverified
	if s := e.settings.CutoffStatus(); s != metalabelsettings.CutoffUnverified {
		t.Fatalf("cutoff status = %q, want unverified (the test's premise)", s)
	}
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 2 {
		t.Fatalf("Run = (%d, %v), want recording to go on under an unverified cutoff", n, err)
	}
	if got := unverified(); got != 2 {
		t.Fatalf("%d labels marked cutoff_unverified, want both", got)
	}

	// Once the model is recorded, later labels are not marked; the marked ones stay marked.
	e.settings.LLMCutoffModel = "m"
	later := e.decision(t, base.Add(2*time.Hour), domain.JevDirectionLong)
	e.bars(t, later.Timestamp, tpBars)
	if n, err := e.recorder.Run(context.Background()); err != nil || n != 1 {
		t.Fatalf("Run = (%d, %v), want 1", n, err)
	}
	if got := unverified(); got != 2 {
		t.Fatalf("%d labels marked cutoff_unverified, want still only the first two", got)
	}
	if got := e.count(t, `SELECT COUNT(*) FROM counterfactual_labels WHERE jev_decision_id = ? AND cutoff_unverified = 0`, later.ID); got != 1 {
		t.Fatalf("the label recorded under a confirmed cutoff is marked")
	}
}
