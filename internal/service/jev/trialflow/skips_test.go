package trialflow_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// skipReasons returns the skip reasons recorded for arm, oldest first.
func (e *env) skipReasons(arm string) []string {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT reason FROM jev_trial_skips WHERE arm_id = ? ORDER BY id`, arm)
	if err != nil {
		e.t.Fatalf("query skips: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *env) wantSkips(arm string, want ...string) {
	e.t.Helper()
	got := e.skipReasons(arm)
	if len(got) != len(want) {
		e.t.Fatalf("skips of %s = %v, want %v", arm, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			e.t.Fatalf("skips of %s = %v, want %v", arm, got, want)
		}
	}
}

func (e *env) trialRows(arm string) int {
	e.t.Helper()
	return e.count(`SELECT COUNT(*) FROM jev_trial_decisions WHERE arm_id = ?`, arm)
}

func TestSkipIsCountedWhenTheArmHasNoKey(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	arm := e.addArm("a1", answer(okResp(), 3000))
	arm.configured = false
	e.enable(defaultArm("a1"))
	e.evaluate()
	e.wantSkips("a1", jevtrial.SkipKeyNotSet)
	if arm.calls() != 0 || e.trialRows("a1") != 0 {
		t.Fatalf("a skipped arm must leave no call and no result row: calls=%d rows=%d", arm.calls(), e.trialRows("a1"))
	}
}

func TestSkipIsCountedForAnUnregisteredPromptVersion(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	e.addArm("a1", answer(okResp(), 3000))
	bad := defaultArm("a1")
	bad.PromptVersion = "trader-v999"
	e.enable(bad)
	e.evaluate()
	e.wantSkips("a1", jevtrial.SkipPromptUnregistered)
	if e.trialRows("a1") != 0 {
		t.Fatal("a skipped arm must leave no result row")
	}
}

func TestSkipIsCountedInsideTheMinimumInterval(t *testing.T) {
	paceNow := time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
	e := newEnv(t, metalabelsettings.ModePaper, nil, trial.WithMinInterval(2*time.Second), trial.WithPaceClock(func() time.Time { return paceNow }))
	e.addArm("a1", answer(okResp(), 3000))
	e.enable(defaultArm("a1"))
	e.evaluate()
	e.evaluate()
	e.wantSkips("a1", jevtrial.SkipMinInterval)
	if e.trialRows("a1") != 1 {
		t.Fatalf("rows = %d, want 1: only the call that was made has a row", e.trialRows("a1"))
	}
}

// A full set of in-flight slots skips the comparison of the arms that did not get
// one, and counts it: the slot-less arms' decisions leave the cohort with a reason.
func TestSkipIsCountedWhenTheInFlightSlotsAreFull(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	open := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(open) }) }
	t.Cleanup(release)
	blocked := func(ctx context.Context, _ jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		select {
		case <-open:
		case <-ctx.Done():
		}
		return okResp(), 80 * time.Millisecond, 3000, nil
	}
	ids := []string{"a1", "a2", "a3", "a4", "a5", "a6"}
	arms := make([]jevtrialsettings.Arm, len(ids))
	for i, id := range ids {
		e.addArm(id, blocked)
		arms[i] = defaultArm(id)
	}
	e.enable(arms...)
	if _, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// The dispatcher takes a slot per arm in order, so a5 and a6 find all four taken.
	waitFor(t, "the two comparisons without a slot to be recorded", func() bool {
		return e.count(`SELECT COUNT(*) FROM jev_trial_skips`) == 2
	})
	release()
	e.trial.Wait()
	e.wantSkips("a5", jevtrial.SkipConcurrencyFull)
	e.wantSkips("a6", jevtrial.SkipConcurrencyFull)
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions`); n != 4 {
		t.Errorf("rows = %d, want 4: the skipped arms have no row", n)
	}
}

func TestSkipReasonFollowsTheStopThatIsInForce(t *testing.T) {
	cases := []struct {
		name  string
		setup func(e *env)
		want  string
	}{
		{"overall cap recorded earlier today", func(e *env) {
			v, _ := jevtrialsettings.EncodeHalt(jevtrialsettings.KeyHalt, jevtrialsettings.Halt{Date: jevshadowsettings.Day(e.clock()), Reason: jevtrialsettings.HaltReasonDailyCostCap, At: "t"})
			e.set(jevtrialsettings.KeyHalt, v)
		}, jevtrial.SkipDailyCapOverall},
		{"arm cap recorded earlier today", func(e *env) {
			v, _ := jevtrialsettings.EncodeHalt("k", jevtrialsettings.Halt{Date: jevshadowsettings.Day(e.clock()), Reason: jevtrialsettings.HaltReasonArmCostCap, At: "t"})
			e.set(jevtrialsettings.HaltKey("a1"), v)
		}, jevtrial.SkipDailyCapArm},
		{"payment required", func(e *env) {
			v, _ := jevtrialsettings.EncodeHalt("k", jevtrialsettings.Halt{Date: jevshadowsettings.Day(e.clock()), Reason: jevtrialsettings.HaltReasonPaymentRequired, At: "t"})
			e.set(jevtrialsettings.KeyHalt, v)
		}, jevtrial.SkipStoppedPaymentRequired},
		{"forbidden", func(e *env) {
			v, _ := jevtrialsettings.EncodeHalt("k", jevtrialsettings.Halt{Date: jevshadowsettings.Day(e.clock()), Reason: jevtrialsettings.HaltReasonForbidden, At: "t"})
			e.set(jevtrialsettings.HaltKey("a1"), v)
		}, jevtrial.SkipStoppedForbidden},
		{"failure cap recorded earlier today", func(e *env) {
			v, _ := jevtrialsettings.EncodeHalt("k", jevtrialsettings.Halt{Date: jevshadowsettings.Day(e.clock()), Reason: jevtrialsettings.HaltReasonDailyFailureCap, At: "t"})
			e.set(jevtrialsettings.HaltKey("a1"), v)
		}, jevtrial.SkipStoppedFailureCap},
		{"failure counter at the cap", func(e *env) {
			v, _ := jevtrialsettings.EncodeFailures("k", jevtrialsettings.Failures{Date: jevshadowsettings.Day(e.clock()), Count: jevtrialsettings.DailyFailureCap})
			e.set(jevtrialsettings.FailuresKey("a1"), v)
		}, jevtrial.SkipStoppedFailureCap},
		{"throttle cooldown", func(e *env) {
			now := e.clock()
			v, _ := jevtrialsettings.EncodeThrottle("k", jevtrialsettings.Throttle{Date: jevshadowsettings.Day(now), Count: 1, Status: 429,
				Reason: jevtrialsettings.ThrottleReason(429), At: now.Format(time.RFC3339), Until: now.Add(time.Hour).Format(time.RFC3339)})
			e.set(jevtrialsettings.ThrottleKey("a1"), v)
		}, jevtrial.SkipThrottled},
		{"overall cap reached by the stored cost", func(e *env) {
			e.addArm("a2", answer(okResp(), 3000))
			e.set(jevtrialsettings.KeyDailyCostCapUSD, "0.000001")
		}, jevtrial.SkipDailyCapOverall},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, metalabelsettings.ModePaper, nil)
			a1 := e.addArm("a1", answer(okResp(), 3000))
			e.enable(defaultArm("a1"))
			if tc.name == "overall cap reached by the stored cost" {
				e.evaluate() // one call whose counted cost already exceeds the tiny cap below
			}
			tc.setup(e)
			before := a1.calls()
			e.evaluate()
			if a1.calls() != before {
				t.Fatal("the arm was called under a stop")
			}
			got := e.skipReasons("a1")
			if len(got) == 0 || got[len(got)-1] != tc.want {
				t.Fatalf("skips = %v, want the last to be %q", got, tc.want)
			}
		})
	}
}

func TestSkipIsCountedForAnArmCapReachedByTheStoredCost(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	costly := okResp()
	costly.ReportedCostUSD = new(0.002)
	a1 := e.addArm("a1", answer(costly, 3000))
	capped := defaultArm("a1")
	capped.DailyCostCapUSD = 0.0018
	e.enable(capped)
	e.evaluate()
	e.evaluate()
	if a1.calls() != 1 {
		t.Fatalf("calls = %d, want 1", a1.calls())
	}
	e.wantSkips("a1", jevtrial.SkipDailyCapArm)
}

func TestSkipIsCountedWhenTheStopRecordsCannotBeRead(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	a1 := e.addArm("a1", answer(okResp(), 3000))
	e.enable(defaultArm("a1"))
	e.settings.mu.Lock()
	e.settings.failGets = map[string]bool{jevtrialsettings.HaltKey("a1"): true}
	e.settings.mu.Unlock()
	e.evaluate()
	if a1.calls() != 0 {
		t.Fatal("an arm whose stop records cannot be read must not be called")
	}
	e.wantSkips("a1", jevtrial.SkipCheckFailed)
}

type failingSkipResults struct{ trial.Results }

func (failingSkipResults) InsertSkip(context.Context, jevtrial.Skip) error {
	return errors.New("disk full")
}

// Recording a skip is best effort: it must never change what the trial does next,
// and a skip is never stored as a result row.
func TestAFailingSkipRecordChangesNothing(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, func(r trial.Results) trial.Results { return failingSkipResults{r} })
	a1 := e.addArm("a1", answer(okResp(), 3000))
	a1.configured = false
	e.enable(defaultArm("a1"))
	if live := e.evaluate(); live.ID == 0 {
		t.Fatal("the live decision must be unaffected by a failing skip record")
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions`) + e.count(`SELECT COUNT(*) FROM jev_trial_skips`); n != 0 {
		t.Fatalf("%d rows written, want none", n)
	}
}

// A comparison that was never meant to run (trial off, no arms, outside Paper) is
// not a skip: nothing is recorded.
func TestNoSkipIsRecordedWhileTheTrialIsOff(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	e.addArm("a1", answer(okResp(), 3000)).configured = false
	e.evaluate() // nothing enabled
	e.enable(defaultArm("a1"))
	e.set(jevtrialsettings.KeyEnabled, "0")
	e.evaluate()
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_skips`); n != 0 {
		t.Fatalf("%d skips recorded while the trial was off", n)
	}
	live := newEnv(t, metalabelsettings.ModeLive, nil)
	live.addArm("a1", answer(okResp(), 3000)).configured = false
	live.enable(defaultArm("a1"))
	live.evaluate()
	if n := live.count(`SELECT COUNT(*) FROM jev_trial_skips`); n != 0 {
		t.Fatalf("%d skips recorded outside Paper", n)
	}
}

// The same (decision, arm) is recorded once.
func TestASkipIsRecordedOncePerDecisionAndArm(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	live := e.evaluate()
	for range 2 {
		if err := e.results.InsertSkip(context.Background(), jevtrial.Skip{JevDecisionID: live.ID, ArmID: "a1", Reason: jevtrial.SkipKeyNotSet, CreatedAt: e.clock()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.results.InsertSkip(context.Background(), jevtrial.Skip{JevDecisionID: live.ID, ArmID: "a1", Reason: "made up"}); err == nil {
		t.Error("an unknown reason must be refused")
	}
	e.wantSkips("a1", jevtrial.SkipKeyNotSet)
}
