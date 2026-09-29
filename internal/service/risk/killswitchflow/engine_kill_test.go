package killswitchflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// Issue #169: POST /system/kill must reach the audit log and force-close
// positions (FR-RISK-3/FR-RISK-5, UC-11).
func TestEngine_Kill_RecordsAuditEventNotifiesAndForceCloses(t *testing.T) {
	closer := &fakeCloser{}
	e, ks := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, closer, nil)
	ctx := context.Background()

	if err := e.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	events, err := ks.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(events) != 1 || events[0].Reason != domain.KillReasonOperatorManual {
		t.Fatalf("ListUnresolved = %+v, want exactly one operator_manual event", events)
	}
	if len(closer.closed) != 1 || closer.closed[0] != domain.KillReasonOperatorManual {
		t.Fatalf("closer.closed = %v, want [operator_manual]", closer.closed)
	}
	if state, _, err := e.State(ctx); err != nil || state != domain.SystemStateKilled {
		t.Fatalf("State = (%q, %v), want killed", state, err)
	}

	// A repeated Kill while still active adds no second audit row or flatten.
	if err := e.Kill(ctx); err != nil {
		t.Fatalf("second Kill: %v", err)
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 1 {
		t.Fatalf("after second Kill events = %d, want 1", len(events))
	}
	if len(closer.closed) != 1 {
		t.Fatalf("closer.closed = %v, want a single flatten", closer.closed)
	}
}

func TestEngine_Kill_ManualResumeOnlyAndResumeWritesResolution(t *testing.T) {
	e, ks := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)
	ctx := context.Background()
	if err := e.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if n, err := e.AutoResume(ctx); err != nil || n != 0 {
		t.Fatalf("AutoResume = (%d, %v), want (0, nil): operator_manual is manual-resume-only", n, err)
	}
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 0 {
		t.Fatalf("unresolved after Resume = %+v", events)
	}
	if state, _, err := e.State(ctx); err != nil || state != domain.SystemStateRunning {
		t.Fatalf("State = (%q, %v), want running", state, err)
	}
	history, err := ks.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(history) != 1 || history[0].ResolvedBy == nil || *history[0].ResolvedBy != domain.ResolvedByManual {
		t.Fatalf("history = %+v, want one event resolved_by manual", history)
	}
}

// The force-close failing must not leave the system running: the flag is
// already set and the audit row exists.
func TestEngine_Kill_StaysKilledWhenForceCloseFails(t *testing.T) {
	e, ks := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, failingCloser{}, nil)
	ctx := context.Background()

	if err := e.Kill(ctx); err == nil {
		t.Fatalf("Kill returned nil, want the force-close error surfaced")
	}
	if state, _, err := e.State(ctx); err != nil || state != domain.SystemStateKilled {
		t.Fatalf("State = (%q, %v), want killed", state, err)
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 1 {
		t.Fatalf("events = %d, want the audit row kept", len(events))
	}
}

type failingCloser struct{}

func (failingCloser) CloseAll(context.Context, string) error { return context.DeadlineExceeded }

// Issues #165/#172: a manual Resume must not be undone by the very history
// that tripped the limit.
func TestEngine_Resume_ConsecutiveLosses_BaselineAvoidsImmediateRefire(t *testing.T) {
	limits := testLimits()
	clock := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	trades := []fakeTrade{}
	for i := 0; i < limits.MaxConsecutiveLosses; i++ {
		trades = append(trades, fakeTrade{closedAt: clock.Add(time.Duration(-30+i) * time.Minute), lossPct: 0.1})
	}
	closer := &fakeCloser{}
	e, ks := newEngine(t, limits, fakePortfolio{trades: trades}, closer, func() time.Time { return clock })
	ctx := context.Background()

	if passed, _ := e.Check(ctx, 1, domain.JevDirectionLong); passed {
		t.Fatalf("Check passed at the loss limit")
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 1 || events[0].Reason != domain.KillReasonConsecutiveLosses {
		t.Fatalf("unresolved = %+v, want one consecutive_losses", events)
	}

	// Long past the last loss's cooldown, the operator resumes.
	clock = clock.Add(6 * time.Hour)
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if !passed {
		t.Fatalf("Check after Resume = (false, %q), want pass: streak must restart", reason)
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 0 {
		t.Fatalf("unresolved after Resume+Check = %+v, want none (no re-fire)", events)
	}
	if len(closer.closed) != 1 {
		t.Fatalf("closer.closed = %v, want only the original flatten", closer.closed)
	}
}

func TestEngine_Resume_ConsecutiveLosses_RefiresOnlyAfterFreshLosses(t *testing.T) {
	limits := testLimits()
	clock := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	var trades []fakeTrade
	for i := 0; i < limits.MaxConsecutiveLosses; i++ {
		trades = append(trades, fakeTrade{closedAt: clock.Add(time.Duration(-30+i) * time.Minute), lossPct: 0.1})
	}
	provider := &mutablePortfolio{fakePortfolio{trades: trades}}
	e, ks := newEngine(t, limits, provider, nil, func() time.Time { return clock })
	ctx := context.Background()

	e.Check(ctx, 1, domain.JevDirectionLong)
	clock = clock.Add(6 * time.Hour)
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// One fewer loss than the limit after the Resume: still allowed.
	for i := 0; i < limits.MaxConsecutiveLosses-1; i++ {
		clock = clock.Add(10 * time.Minute)
		provider.trades = append(provider.trades, fakeTrade{closedAt: clock, lossPct: 0.1})
	}
	clock = clock.Add(6 * time.Hour) // clear the cooldown gate
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check with limit-1 fresh losses = (false, %q), want pass", reason)
	}

	// The limit-th fresh loss re-fires (cooldown long elapsed, so the streak
	// limit is what rejects).
	provider.trades = append(provider.trades, fakeTrade{closedAt: clock.Add(-5 * time.Hour), lossPct: 0.1})
	if passed, _ := e.Check(ctx, 1, domain.JevDirectionLong); passed {
		t.Fatalf("Check passed at the limit of fresh losses")
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 1 || events[0].Reason != domain.KillReasonConsecutiveLosses {
		t.Fatalf("unresolved = %+v, want one new consecutive_losses", events)
	}
}

// The baselines are independent: resuming from a loss streak does not
// forgive today's realized loss, and resuming from a daily-loss halt does
// not reset the streak.
func TestEngine_Resume_BaselinesAreIndependentPerLimit(t *testing.T) {
	limits := testLimits()
	clock := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	dailyLoss := fakeTrade{closedAt: clock.Add(-30 * time.Minute), lossPct: limits.MaxDailyLossPct}

	e, ks := newEngine(t, limits, fakePortfolio{trades: []fakeTrade{dailyLoss}}, nil, func() time.Time { return clock })
	ctx := context.Background()
	if passed, _ := e.Check(ctx, 1, domain.JevDirectionLong); passed {
		t.Fatalf("Check passed at the daily loss limit")
	}
	clock = clock.Add(6 * time.Hour)
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if passed, reason := e.Check(ctx, 1, domain.JevDirectionLong); !passed {
		t.Fatalf("Check after daily-loss Resume = (false, %q), want pass", reason)
	}
	if events, _ := ks.ListUnresolved(ctx); len(events) != 0 {
		t.Fatalf("unresolved = %+v, want none", events)
	}

	// A manual Kill Resume records neither baseline: the loss history is
	// still counted from the beginning, so the halt is reproducible.
	e2, _ := newEngine(t, limits, fakePortfolio{trades: []fakeTrade{dailyLoss}}, nil, func() time.Time { return clock })
	if err := e2.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := e2.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if passed, _ := e2.Check(ctx, 1, domain.JevDirectionLong); passed {
		t.Fatalf("Check passed after Kill+Resume, want the daily loss limit still enforced")
	}
}

// mutablePortfolio lets a test append trades after the Engine is built.
type mutablePortfolio struct{ fakePortfolio }
