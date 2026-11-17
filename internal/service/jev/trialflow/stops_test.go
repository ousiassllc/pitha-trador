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

// meeting answers every call with resp once n calls are in flight together, so
// the n arms of one decision have all passed their cap checks (and hold their
// reservations) before any of them stores a row. It is a cyclic barrier: the
// next decision's calls meet again. The 10 s is a bound that only an arm that is
// never called ever reaches.
type meeting struct {
	mu      sync.Mutex
	n       int
	arrived int
	release chan struct{}
}

func newMeeting(n int) *meeting { return &meeting{n: n, release: make(chan struct{})} }

func (m *meeting) answer(resp trialwire.Response, bytes int) func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
	return func(ctx context.Context, _ jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		m.mu.Lock()
		gate := m.release
		if m.arrived++; m.arrived == m.n {
			close(gate)
			m.arrived, m.release = 0, make(chan struct{})
		}
		m.mu.Unlock()
		select {
		case <-gate:
			return resp, 80 * time.Millisecond, bytes, nil
		case <-ctx.Done():
			return trialwire.Response{}, 0, 0, ctx.Err()
		case <-time.After(10 * time.Second):
			return trialwire.Response{}, 0, 0, errors.New("the other arm was never called in parallel")
		}
	}
}

func (e *env) halt(key string) *jevtrialsettings.Halt {
	e.t.Helper()
	raw, ok := e.get(key)
	if !ok {
		return nil
	}
	h, err := jevtrialsettings.ParseHalt(key, raw)
	if err != nil {
		e.t.Fatal(err)
	}
	return &h
}

func TestOverallCapStopsEveryArmAndResumesTheNextJSTDay(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	costly := okResp()
	costly.ReportedCostUSD = new(0.004)
	// The two arms of one decision must both be called: they meet inside the call, so
	// both have passed their cap check before either row is stored. Without it, an
	// arm that checks after the other stored its row while the other's reservation is
	// still held counts that call twice (the safe side, see reserved) and may be
	// skipped: 0.004 stored + 0.0016 + 0.0016 reserved exceeds the cap.
	meet := newMeeting(2)
	a1, a2 := e.addArm("a1", meet.answer(costly, 3000)), e.addArm("a2", meet.answer(costly, 3000))
	e.enable(defaultArm("a1"), defaultArm("a2"))
	// A call is reserved at about 0.0016 (its request at the worst-case price): both
	// reservations fit (0.0032), and the two calls together reach the cap (0.008).
	e.set(jevtrialsettings.KeyDailyCostCapUSD, "0.006")

	e.evaluate() // both are called (nothing spent yet): 0.008 counted
	e.evaluate() // the overall cap is reached: neither is called
	if a1.calls() != 1 || a2.calls() != 1 {
		t.Fatalf("calls a1=%d a2=%d, want 1 each: the overall cap must stop every arm", a1.calls(), a2.calls())
	}
	h := e.halt(jevtrialsettings.KeyHalt)
	if h == nil || h.Reason != jevtrialsettings.HaltReasonDailyCostCap || h.CapUSD != 0.006 || h.SpentUSD < 0.008-1e-9 {
		t.Fatalf("halt record = %+v", h)
	}
	e.advance(24 * time.Hour) // the next JST day
	e.evaluate()
	if a1.calls() != 2 || a2.calls() != 2 {
		t.Fatalf("calls a1=%d a2=%d, want 2 each after the day changed", a1.calls(), a2.calls())
	}
}

func TestArmCapStopsThatArmOnly(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	costly := okResp()
	costly.ReportedCostUSD = new(0.002)
	a1, a2 := e.addArm("a1", answer(costly, 3000)), e.addArm("a2", answer(costly, 3000))
	capped := defaultArm("a1")
	capped.DailyCostCapUSD = 0.0018 // room for one reservation (about 0.0015), reached by the call's cost
	e.enable(capped, defaultArm("a2"))

	e.evaluate()
	e.evaluate()
	if a1.calls() != 1 || a2.calls() != 2 {
		t.Fatalf("calls a1=%d a2=%d, want 1 and 2: the arm cap stops that arm only", a1.calls(), a2.calls())
	}
	if h := e.halt(jevtrialsettings.HaltKey("a1")); h == nil || h.Reason != jevtrialsettings.HaltReasonArmCostCap {
		t.Errorf("arm halt = %+v", h)
	}
	if e.halt(jevtrialsettings.KeyHalt) != nil || e.halt(jevtrialsettings.HaltKey("a2")) != nil {
		t.Error("only the capped arm may be halted")
	}
}

func TestPaymentRequiredHaltsTheWholeTrialForTheDay(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	a1 := e.addArm("a1", fail(&jev.APIError{StatusCode: 402, Body: "insufficient credits"}, 3000))
	a2 := e.addArm("a2", answer(okResp(), 3000))
	e.enable(defaultArm("a1"), defaultArm("a2"))

	// The arms of one decision run in parallel: a2 may or may not have passed its
	// check before a1's 402 landed, so it is called once or not at all for the first
	// decision; for the second decision neither is called.
	e.evaluate()
	firstA2 := a2.calls()
	e.evaluate()
	if a1.calls() != 1 || firstA2 > 1 || a2.calls() != firstA2 {
		t.Fatalf("calls a1=%d a2=%d (%d after the first decision), want a1 once and a2 not again: a 402 stops every arm", a1.calls(), a2.calls(), firstA2)
	}
	if h := e.halt(jevtrialsettings.KeyHalt); h == nil || h.Reason != jevtrialsettings.HaltReasonPaymentRequired || h.Status != 402 {
		t.Fatalf("halt = %+v", h)
	}
	if got := e.rowOf("a1"); got.FailureCode != jevtrial.FailureHTTP402 {
		t.Errorf("failure_code = %q, the 402 must still be a row", got.FailureCode)
	}
	e.advance(24 * time.Hour)
	e.evaluate()
	// a1 answers 402 again, so a2 races with that stop once more: it is called at most once.
	if a1.calls() != 2 || a2.calls() < firstA2 || a2.calls() > firstA2+1 {
		t.Fatalf("calls a1=%d a2=%d, want a1 called again the next JST day and a2 at most once more than %d", a1.calls(), a2.calls(), firstA2)
	}
}

func TestForbiddenHaltsThatArmOnly(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	a1 := e.addArm("a1", fail(&jev.APIError{StatusCode: 403, Body: "model not allowed"}, 3000))
	a2 := e.addArm("a2", answer(okResp(), 3000))
	e.enable(defaultArm("a1"), defaultArm("a2"))

	e.evaluate()
	e.evaluate()
	if a1.calls() != 1 || a2.calls() != 2 {
		t.Fatalf("calls a1=%d a2=%d, want 1 and 2", a1.calls(), a2.calls())
	}
	if h := e.halt(jevtrialsettings.HaltKey("a1")); h == nil || h.Reason != jevtrialsettings.HaltReasonForbidden || h.Status != 403 {
		t.Fatalf("arm halt = %+v", h)
	}
	if e.halt(jevtrialsettings.KeyHalt) != nil {
		t.Error("a 403 must not halt the whole trial")
	}
}

func TestThrottleStopsTheArmForTheCooldownWithoutRetry(t *testing.T) {
	for _, status := range []int{429, 529} {
		e := newEnv(t, metalabelsettings.ModePaper, nil)
		a1 := e.addArm("a1", fail(&jev.APIError{StatusCode: status, Body: "busy"}, 3000))
		a2 := e.addArm("a2", answer(okResp(), 3000))
		e.enable(defaultArm("a1"), defaultArm("a2"))

		e.evaluate()
		if a1.calls() != 1 {
			t.Fatalf("%d: calls = %d, want exactly 1 (no retry)", status, a1.calls())
		}
		raw, ok := e.get(jevtrialsettings.ThrottleKey("a1"))
		if !ok {
			t.Fatalf("%d: no throttle record", status)
		}
		if rec, err := jevtrialsettings.ParseThrottle("k", raw); err != nil || rec.Status != status || rec.Count != 1 {
			t.Fatalf("%d: throttle record %+v (%v)", status, rec, err)
		}
		if _, counted := e.get(jevtrialsettings.FailuresKey("a1")); counted {
			t.Errorf("%d: a throttle must not count against the failure cap", status)
		}
		if row := e.rowOf("a1"); row.Status != jevtrial.StatusFailed || *row.HTTPStatus != status {
			t.Errorf("%d: row = %+v, a throttle is still a failed row", status, row)
		}
		e.advance(jevtrialsettings.ThrottleCooldown - time.Minute)
		e.evaluate()
		if a1.calls() != 1 || a2.calls() != 2 {
			t.Fatalf("%d: inside the cooldown calls a1=%d a2=%d, want 1 and 2", status, a1.calls(), a2.calls())
		}
		e.advance(2 * time.Minute)
		e.evaluate()
		if a1.calls() != 2 {
			t.Fatalf("%d: after the cooldown calls = %d, want 2", status, a1.calls())
		}
	}
}

func TestFailuresAreCountedAndTheFailureCapStopsTheArm(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	a1 := e.addArm("a1", fail(&jev.APIError{StatusCode: 500, Body: "boom"}, 3000))
	e.enable(defaultArm("a1"))
	for range 3 {
		e.evaluate()
	}
	raw, _ := e.get(jevtrialsettings.FailuresKey("a1"))
	if f, err := jevtrialsettings.ParseFailures("k", raw); err != nil || f.Count != 3 || f.Date != jevshadowsettings.Day(e.clock()) {
		t.Fatalf("failure counter = %+v (%v), want 3 today", f, err)
	}
	// At the cap the arm stops for the day.
	capValue, _ := jevtrialsettings.EncodeFailures("k", jevtrialsettings.Failures{Date: jevshadowsettings.Day(e.clock()), Count: jevtrialsettings.DailyFailureCap})
	e.set(jevtrialsettings.FailuresKey("a1"), capValue)
	before := a1.calls()
	e.evaluate()
	if a1.calls() != before {
		t.Fatal("the arm was called at the failure cap")
	}
	if h := e.halt(jevtrialsettings.HaltKey("a1")); h == nil || h.Reason != jevtrialsettings.HaltReasonDailyFailureCap {
		t.Fatalf("arm halt = %+v", h)
	}
	e.advance(24 * time.Hour)
	e.evaluate()
	if a1.calls() != before+1 {
		t.Fatal("the arm did not resume the next day")
	}
}

func TestPacingDropsACallLessThanTheMinimumIntervalAfterTheArmsPrevious(t *testing.T) {
	paceNow := time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC)
	e := newEnv(t, metalabelsettings.ModePaper, nil, trial.WithMinInterval(2*time.Second), trial.WithPaceClock(func() time.Time { return paceNow }))
	a1 := e.addArm("a1", answer(okResp(), 3000))
	e.enable(defaultArm("a1"))
	e.evaluate()
	e.evaluate()
	if a1.calls() != 1 {
		t.Fatalf("calls = %d, want 1: the second comparison is inside the interval", a1.calls())
	}
	paceNow = paceNow.Add(3 * time.Second)
	e.evaluate()
	if a1.calls() != 2 {
		t.Fatalf("calls = %d, want 2 after the interval", a1.calls())
	}
}
