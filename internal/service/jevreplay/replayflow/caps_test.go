package replayflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

// seedTrialRow stores a live-trial row counted at counted USD today, as the
// trial would have, so the shared daily cap sees it.
func (e *env) seedTrialRow(liveID int64, armID string, counted float64) {
	e.t.Helper()
	d := trial.BuildDecision(arm(armID, ""), liveID, 100, trial.CallOutcome{Response: okResp(counted), RequestBytes: 4000}, now, nil)
	if _, err := e.trial.Insert(context.Background(), d); err != nil {
		e.t.Fatalf("seed trial row: %v", err)
	}
}

// Completion criterion: once the daily cost cap is reached no new call is issued.
// The live trial's spend today counts against the same cap.
func TestRunner_NoCallOnceTheSharedDailyCapIsReached(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	live := e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
	e.decide(time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC), state(2))
	run, _ := e.start(wholeOctober(10))
	e.setCap(0.01)
	e.seedTrialRow(live, "trial-only", 0.01) // the trial alone used up the overall cap today

	r := e.runner()
	tick(t, r)
	tick(t, r)

	if n := e.arms["a"].calls(); n != 0 {
		t.Errorf("the arm was called %d times after the cap was reached, want 0", n)
	}
	got := e.run(run.ID)
	if got.Status != jevreplay.StatusRunning || got.BlockedReason != jevtrial.SkipDailyCapOverall || got.CallsDone != 0 {
		t.Errorf("run = %s blocked %q done %d, want running, blocked by %s, 0 done", got.Status, got.BlockedReason, got.CallsDone, jevtrial.SkipDailyCapOverall)
	}
}

// The replay's own spend reaches the cap too: with room for one worst-case call
// only one is made, and later passes make none.
func TestRunner_StopsCallingWhenItsOwnSpendReachesTheCap(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	for i := range 3 {
		e.decide(time.Date(2026, 10, 5+i, 1, 0, 0, 0, time.UTC), state(i))
	}
	run, plan := e.start(wholeOctober(10))
	perCall := plan.Calls[0].ReservedUSD
	e.setCap(perCall * 1.5) // one reservation fits; a second on top of the first does not
	e.configure(perCall, arm("a", ""))

	r := e.runner()
	tick(t, r)
	first := e.arms["a"].calls()
	tick(t, r)

	if first != 1 || e.arms["a"].calls() != 1 {
		t.Errorf("calls after the first pass %d and the second %d, want 1 and 1", first, e.arms["a"].calls())
	}
	got := e.run(run.ID)
	if got.Status != jevreplay.StatusRunning || got.BlockedReason != jevtrial.SkipDailyCapOverall || got.CallsDone != 1 {
		t.Errorf("run = %s blocked %q done %d, want running, blocked by the cap, 1 done", got.Status, got.BlockedReason, got.CallsDone)
	}
}

// An arm's own daily cap holds the arm back without stopping the others.
func TestRunner_ArmCapHoldsOnlyThatArm(t *testing.T) {
	e := newEnv(t)
	e.enable()
	a := arm("a", "")
	a.DailyCostCapUSD = 0.0001
	e.configure(0.000005, a, arm("b", ""))
	live := e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
	run, _ := e.start(wholeOctober(10))
	e.seedTrialRow(live, "a", 0.0001) // arm a is at its own cap today through the trial's rows

	tick(t, e.runner())

	if e.arms["a"].calls() != 0 || e.arms["b"].calls() != 1 {
		t.Errorf("calls a=%d b=%d, want 0 and 1", e.arms["a"].calls(), e.arms["b"].calls())
	}
	if got := e.run(run.ID); got.Status != jevreplay.StatusRunning || got.BlockedReason != jevtrial.SkipDailyCapArm {
		t.Errorf("run = %s blocked %q, want running, blocked by the arm cap", got.Status, got.BlockedReason)
	}
}

// A run never counts more than its estimate's worst case: with room for one
// reservation only one call is made and the run ends stopped.
func TestRunner_RunCostCapEndsTheRun(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	for i := range 3 {
		e.decide(time.Date(2026, 10, 5+i, 1, 0, 0, 0, time.UTC), state(i))
	}
	run, plan := e.start(wholeOctober(10))
	perCall := plan.Calls[0].ReservedUSD
	e.configure(perCall, arm("a", ""))
	if _, err := e.db.Exec(`UPDATE jev_replay_runs SET estimate_max_cost_usd = ? WHERE id = ?`, perCall*1.5, run.ID); err != nil {
		t.Fatal(err)
	}

	tick(t, e.runner())

	got := e.run(run.ID)
	if got.Status != jevreplay.StatusStopped || got.StopReason != jevreplay.StopRunCostCap {
		t.Errorf("run = %s (%s), want stopped by its own cost cap", got.Status, got.StopReason)
	}
	if n := e.arms["a"].calls(); n != 1 || got.CallsDone != 1 {
		t.Errorf("%d calls made and %d recorded, want 1 and 1", n, got.CallsDone)
	}
}
