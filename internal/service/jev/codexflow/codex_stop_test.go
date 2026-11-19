package codexflow_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// A usage limit stops the whole provider until the time the CLI gave and is never
// retried: not after 15 minutes (the 429 cooldown), not by the next decisions; it
// runs again after the reset time. No throttle record and no failed-call count is
// written for it.
func TestCodexUsageLimitStopsTheProviderUntilTheResetTimeWithoutRetry(t *testing.T) {
	e := newEnv(t)
	reset := e.clock().Add(3 * time.Hour)
	limit := &codexwire.CallError{Reason: codexwire.ReasonTurn, UsageLimit: true, Until: reset}
	a1, a2 := e.addArm("cx1", failedCall(limit)), e.addArm("cx2", answer(codexResp(), 2000))
	e.enable(codexArm("cx1"), codexArm("cx2"))
	e.enableCodex("", "")

	e.evaluate()
	if a1.calls() != 1 || a2.calls() != 0 {
		t.Fatalf("calls cx1=%d cx2=%d: the failure must stop the provider before its next arm", a1.calls(), a2.calls())
	}
	d := e.rowOf("cx1")
	if d.Status != jevtrial.StatusFailed || d.FailureCode != jevtrial.FailureUsageLimit || d.HTTPStatus != nil {
		t.Fatalf("row = status %s code %s http %v", d.Status, d.FailureCode, d.HTTPStatus)
	}
	h := e.codexHalt()
	if h == nil || h.Reason != jevtrialsettings.CodexHaltUsageLimit || !h.UntilKnown || h.Until != reset.UTC().Format(time.RFC3339) {
		t.Fatalf("halt = %+v, want a usage_limit stop until %s", h, reset.UTC().Format(time.RFC3339))
	}
	if e.skips("cx2", jevtrial.SkipStoppedUsageLimit) != 1 {
		t.Error("the other Codex arm's comparison must be skipped as stopped_usage_limit")
	}
	if _, ok := e.get(jevtrialsettings.ThrottleKey("cx1")); ok {
		t.Error("a usage limit must not use the 429/529 cooldown record")
	}
	if _, ok := e.get(jevtrialsettings.FailuresKey("cx1")); ok {
		t.Error("a usage limit is not a failed-call count")
	}

	e.advance(16 * time.Minute) // past the old 15-minute cooldown: still stopped
	e.evaluate()
	e.advance(2 * time.Hour) // still before the reset time
	e.evaluate()
	if a1.calls() != 1 || a2.calls() != 0 {
		t.Fatalf("calls cx1=%d cx2=%d, want no call before the reset time", a1.calls(), a2.calls())
	}
	e.advance(time.Hour) // the reset time
	e.evaluate()
	if a1.calls() != 2 {
		t.Fatalf("cx1 calls = %d, want it called again after the reset time", a1.calls())
	}
}

// Without a usable reset time the provider stops until the end of the JST day.
func TestCodexUsageLimitWithoutATimeStopsUntilTheEndOfTheDay(t *testing.T) {
	e := newEnv(t) // 10:00 JST
	a := e.addArm("cx", failedCall(&codexwire.CallError{Reason: codexwire.ReasonTurn, UsageLimit: true}))
	e.enable(codexArm("cx"))
	e.enableCodex("", "")
	e.evaluate()
	h := e.codexHalt()
	if h == nil || h.UntilKnown || h.Until != "2026-11-13T15:00:00Z" { // 00:00 JST of the 14th
		t.Fatalf("halt = %+v, want the end of the JST day, not known", h)
	}
	e.advance(10 * time.Hour) // 20:00 JST: still the same day
	e.evaluate()
	if a.calls() != 1 {
		t.Fatalf("calls = %d, want 1 until the day ends", a.calls())
	}
	e.advance(5 * time.Hour) // 01:00 JST of the next day
	e.evaluate()
	if a.calls() != 2 {
		t.Fatalf("calls = %d, want 2 on the next day", a.calls())
	}
}

// Any failure stops the provider by default (never a retry); only a text that
// matched a usage-limit message is called a usage limit.
func TestCodexAnyFailureStopsTheProviderAndIsNotCalledAUsageLimit(t *testing.T) {
	for name, err := range map[string]error{
		"turn.failed":   &codexwire.CallError{Reason: codexwire.ReasonTurn},
		"non-zero exit": &codexwire.CallError{Reason: codexwire.ReasonExit},
		"no start":      &codexwire.CallError{Reason: codexwire.ReasonStart},
		"timeout":       &codexwire.CallError{Reason: codexwire.ReasonCancelled, UsageLimit: false},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			a := e.addArm("cx", failedCall(err))
			e.enable(codexArm("cx"))
			e.enableCodex("", "")
			e.evaluate()
			e.evaluate()
			if a.calls() != 1 {
				t.Fatalf("calls = %d, want 1: a failure stops the provider, no retry", a.calls())
			}
			if h := e.codexHalt(); h == nil || h.Reason != jevtrialsettings.CodexHaltFailed {
				t.Fatalf("halt = %+v, want reason failed", h)
			}
			if d := e.rowOf("cx"); d.FailureCode == jevtrial.FailureUsageLimit {
				t.Error("a failure whose text matched no usage-limit message must not be labelled usage_limit")
			}
			if e.skips("cx", jevtrial.SkipStoppedProviderFailed) != 1 {
				t.Error("the next comparison must be a stopped_provider_failed skip")
			}
		})
	}
}

func TestCodexDeadlineIsATimeoutFailure(t *testing.T) {
	e := newEnv(t)
	e.addArm("cx", func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		return trialwire.Response{}, time.Second, 2000, context.DeadlineExceeded
	})
	e.enable(codexArm("cx"))
	e.enableCodex("", "")
	e.evaluate()
	if d := e.rowOf("cx"); d.FailureCode != jevtrial.FailureTimeout {
		t.Errorf("failure code = %q, want timeout", d.FailureCode)
	}
	if h := e.codexHalt(); h == nil || h.Reason != jevtrialsettings.CodexHaltFailed {
		t.Errorf("halt = %+v", h)
	}
}

// An answer that is not a valid answer set means the CLI worked: it is one failed
// call of the arm (the failure counter), not a stop of the provider.
func TestCodexInvalidAnswerIsCountedNotStopped(t *testing.T) {
	e := newEnv(t)
	a := e.addArm("cx", failedCall(errors.Join(systemone.ErrInvalidResponse, errors.New("bad"))))
	e.enable(codexArm("cx"))
	e.enableCodex("", "")
	e.evaluate()
	e.evaluate()
	if a.calls() != 2 || e.codexHalt() != nil {
		t.Fatalf("calls = %d halt = %+v, want 2 calls and no provider stop", a.calls(), e.codexHalt())
	}
	if d := e.rows()[0]; d.FailureCode != jevtrial.FailureInvalidResponse || d.CostSource != jevtrial.CostSubscription {
		t.Errorf("row = %+v", d)
	}
}

// Not logged in with ChatGPT (logged out, an API key, no CLI): no call, the state is
// recorded for the screen and the MCP.
func TestCodexNotLoggedInIsNotCalledAndTheStateIsRecorded(t *testing.T) {
	for _, state := range []string{jevtrialsettings.CodexLoginLoggedOut, jevtrialsettings.CodexLoginOtherAuth, jevtrialsettings.CodexLoginUnavailable} {
		e := newEnv(t)
		a := e.addArm("cx", answer(codexResp(), 2000))
		a.login = state
		e.enable(codexArm("cx"))
		e.enableCodex("", "")
		e.evaluate()
		if a.calls() != 0 || e.skips("cx", jevtrial.SkipNotLoggedIn) != 1 {
			t.Fatalf("%s: calls = %d, want none and a not_logged_in skip", state, a.calls())
		}
		raw, ok := e.get(jevtrialsettings.KeyCodexLogin)
		if !ok {
			t.Fatalf("%s: the login state was not recorded", state)
		}
		if l, err := jevtrialsettings.ParseCodexLogin(raw); err != nil || l.State != state {
			t.Errorf("%s: recorded %q (%v)", state, raw, err)
		}
	}
}

// A Codex client without a dedicated CODEX_HOME is not Configured: never called.
func TestCodexArmWithoutADedicatedHomeIsNotCalled(t *testing.T) {
	e := newEnv(t)
	a := e.addArm("cx", answer(codexResp(), 2000))
	a.configured = false
	e.enable(codexArm("cx"))
	e.enableCodex("", "")
	e.evaluate()
	if a.calls() != 0 || e.skips("cx", jevtrial.SkipKeyNotSet) != 1 {
		t.Fatalf("calls = %d, want none and a key_not_set skip", a.calls())
	}
}

// One Codex call at a time: the arms of a decision run one after the other, and a
// decision that finds the provider busy skips its Codex arms (provider_busy) rather
// than running beside the call in flight. OpenRouter arms are not held back.
func TestCodexRunsOneCallAtATime(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	running, maxRunning := 0, 0
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	slow := func(context.Context, jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		mu.Lock()
		running++
		maxRunning = max(maxRunning, running)
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		running--
		mu.Unlock()
		return codexResp(), time.Second, 2000, nil
	}
	c1, c2 := e.addArm("cx1", slow), e.addArm("cx2", slow)
	or := e.addArm("or", answer(okResp(), 3000))
	e.enable(codexArm("cx1"), codexArm("cx2"), defaultArm("or"))
	e.enableCodex("", "")

	if _, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock())); err != nil {
		t.Fatal(err)
	}
	<-entered // cx1 is running
	e.advance(time.Second)
	// A second live decision while cx1 runs: its Codex arms are skipped, its OpenRouter arm is not.
	if _, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock())); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the OpenRouter arm of the second decision", func() bool { return or.calls() == 2 })
	waitFor(t, "the Codex skips", func() bool {
		return e.skips("cx1", jevtrial.SkipProviderBusy) == 1 && e.skips("cx2", jevtrial.SkipProviderBusy) == 1
	})
	close(release)
	e.trial.Wait()
	if maxRunning != 1 {
		t.Fatalf("Codex calls ran %d at once, want 1", maxRunning)
	}
	if c1.calls() != 1 || c2.calls() != 1 {
		t.Fatalf("calls cx1=%d cx2=%d, want the first decision's arms to run one after the other", c1.calls(), c2.calls())
	}
}

// Shutdown while a Codex call runs: no row, no stop — a cancelled call is not a failure of the provider.
func TestCodexCloseCancelsTheCallWithoutAStop(t *testing.T) {
	e := newEnv(t)
	started := make(chan struct{})
	e.addArm("cx", func(ctx context.Context, _ jev.TraderRequest) (trialwire.Response, time.Duration, int, error) {
		close(started)
		<-ctx.Done()
		return trialwire.Response{}, 0, 2000, ctx.Err()
	})
	e.enable(codexArm("cx"))
	e.enableCodex("", "")
	if _, err := e.trader.Evaluate(context.Background(), e.instID, state(e.clock())); err != nil {
		t.Fatal(err)
	}
	<-started
	e.trial.Close()
	if len(e.rows()) != 0 || e.codexHalt() != nil {
		t.Fatalf("rows=%d halt=%+v, want neither after a shutdown", len(e.rows()), e.codexHalt())
	}
}
