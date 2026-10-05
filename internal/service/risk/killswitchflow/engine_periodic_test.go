package killswitchflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// Issue #191: the daily-loss / consecutive-loss Kill Switches must fire
// from the periodic monitor even when no candidate ever reaches Check.
func TestEngine_RunPeriodicChecks_DailyLossLimit_TriggersWithoutSignal(t *testing.T) {
	limits := testLimits()
	clock := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	trade := fakeTrade{closedAt: clock.Add(-time.Hour), lossPct: limits.MaxDailyLossPct}
	closer := &fakeCloser{}
	e, ks := newEngine(t, limits, fakePortfolio{trades: []fakeTrade{trade}}, closer, func() time.Time { return clock })
	ctx := context.Background()

	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || got[0] != domain.KillReasonDailyLossLimit {
		t.Fatalf("unresolved = %v, want [daily_loss_limit]", got)
	}
	if len(closer.closed) != 1 || closer.closed[0] != domain.KillReasonDailyLossLimit {
		t.Fatalf("closer.closed = %v, want one daily_loss_limit flatten", closer.closed)
	}

	// Idempotent: the next tick adds neither an event nor a second flatten.
	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks (2nd): %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || len(closer.closed) != 1 {
		t.Fatalf("after 2nd tick: unresolved=%v closed=%v, want unchanged", got, closer.closed)
	}
}

func TestEngine_RunPeriodicChecks_ConsecutiveLosses_TriggersWithoutSignalAndHonoursBaseline(t *testing.T) {
	limits := testLimits()
	clock := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	var trades []fakeTrade
	for i := 0; i < limits.MaxConsecutiveLosses; i++ {
		trades = append(trades, fakeTrade{closedAt: clock.Add(time.Duration(-30+i) * time.Minute), lossPct: 0.1})
	}
	closer := &fakeCloser{}
	e, ks := newEngine(t, limits, fakePortfolio{trades: trades}, closer, func() time.Time { return clock })
	ctx := context.Background()

	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || got[0] != domain.KillReasonConsecutiveLosses {
		t.Fatalf("unresolved = %v, want [consecutive_losses]", got)
	}
	if len(closer.closed) != 1 || closer.closed[0] != domain.KillReasonConsecutiveLosses {
		t.Fatalf("closer.closed = %v, want one consecutive_losses flatten", closer.closed)
	}

	// The manual-resume baseline is respected: no re-fire on old history.
	clock = clock.Add(time.Hour)
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks after Resume: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 0 || len(closer.closed) != 1 {
		t.Fatalf("after Resume: unresolved=%v closed=%v, want none/one", got, closer.closed)
	}
}

// Below both limits the periodic monitor stays quiet.
func TestEngine_RunPeriodicChecks_BelowLossLimits_NoKillSwitch(t *testing.T) {
	limits := testLimits()
	clock := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	trades := []fakeTrade{{closedAt: clock.Add(-time.Hour), lossPct: limits.MaxDailyLossPct / 2}}
	e, ks := newEngine(t, limits, fakePortfolio{trades: trades}, nil, func() time.Time { return clock })

	if err := e.RunPeriodicChecks(context.Background()); err != nil {
		t.Fatalf("RunPeriodicChecks: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 0 {
		t.Fatalf("unresolved = %v, want none", got)
	}
}

// flakyCloser fails its first failures CloseAll calls, then succeeds.
type flakyCloser struct {
	failures int
	calls    []string
}

func (f *flakyCloser) CloseAll(_ context.Context, reason string) error {
	f.calls = append(f.calls, reason)
	if len(f.calls) <= f.failures {
		return errors.New("db locked")
	}
	return nil
}

// Issue #186: a failed forced liquidation is retried by the periodic
// monitor while the Kill Switch is unresolved and positions remain.
func TestEngine_RunPeriodicChecks_RetriesForceCloseWhilePositionsRemain(t *testing.T) {
	closer := &flakyCloser{failures: 1}
	portfolio := &mutablePortfolio{fakePortfolio{openPositions: 2}}
	e, _ := newEngine(t, testLimits(), portfolio, closer, nil)
	ctx := context.Background()

	if err := e.Kill(ctx); err == nil {
		t.Fatalf("Kill returned nil, want the first force-close failure")
	}
	if len(closer.calls) != 1 {
		t.Fatalf("calls after Kill = %v, want 1", closer.calls)
	}
	// A repeated Kill does not flatten again (idempotent per reason)...
	_ = e.Kill(ctx)
	if len(closer.calls) != 1 {
		t.Fatalf("calls after repeated Kill = %v, want still 1", closer.calls)
	}

	// ...but the periodic sweep does, with the unresolved event's reason.
	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks: %v", err)
	}
	if len(closer.calls) != 2 || closer.calls[1] != domain.KillReasonOperatorManual {
		t.Fatalf("calls = %v, want a retry with operator_manual", closer.calls)
	}

	// Positions gone: the sweep stops calling CloseAll.
	portfolio.openPositions = 0
	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks (flat): %v", err)
	}
	if len(closer.calls) != 2 {
		t.Fatalf("calls = %v, want no further retry once flat", closer.calls)
	}
}

func TestEngine_RunPeriodicChecks_ForceCloseRetryFailureIsReturnedAndRetriedAgain(t *testing.T) {
	closer := &flakyCloser{failures: 3}
	e, _ := newEngine(t, testLimits(), fakePortfolio{openPositions: 1}, closer, nil)
	ctx := context.Background()
	_ = e.Kill(ctx)

	if err := e.RunPeriodicChecks(ctx); err == nil {
		t.Fatalf("RunPeriodicChecks returned nil, want the retry failure surfaced")
	}
	if err := e.RunPeriodicChecks(ctx); err == nil {
		t.Fatalf("RunPeriodicChecks returned nil on 3rd failure")
	}
	if err := e.RunPeriodicChecks(ctx); err != nil {
		t.Fatalf("RunPeriodicChecks after recovery: %v", err)
	}
	if len(closer.calls) != 4 {
		t.Fatalf("calls = %v, want 4 (Kill + 3 sweeps)", closer.calls)
	}
}

// Kill reasons that do not force-close (market_data_down) never trigger
// the sweep, and neither does a resolved event.
func TestEngine_RunPeriodicChecks_NoForceCloseRetryForNonForceCloseOrResolved(t *testing.T) {
	closer := &fakeCloser{}
	e, ks := newEngine(t, testLimits(), fakePortfolio{openPositions: 3}, closer, nil)
	ctx := context.Background()

	if _, err := e.TriggerKillSwitch(ctx, domain.KillReasonMarketDataDown, map[string]any{}); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}
	if err := e.RetryForceClose(ctx); err != nil {
		t.Fatalf("RetryForceClose: %v", err)
	}
	if len(closer.closed) != 0 {
		t.Fatalf("closed = %v, want none for market_data_down", closer.closed)
	}

	if err := e.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 0 {
		t.Fatalf("unresolved = %v", got)
	}
	if err := e.RetryForceClose(ctx); err != nil {
		t.Fatalf("RetryForceClose after Resume: %v", err)
	}
	if len(closer.closed) != 1 {
		t.Fatalf("closed = %v, want only Kill's own flatten", closer.closed)
	}
}

// Issue #185: an orphan FILLED order still inside orphanFillLookback must
// not re-fire fill_discrepancy (and re-liquidate) after a manual Resume.
func TestEngine_Resume_FillDiscrepancy_OrphanDoesNotRefire(t *testing.T) {
	db := newTestDB(t)
	orders := trading.NewOrderRepository(db)
	ks := system.NewKillSwitchRepository(db)
	clock := time.Now().UTC()
	closer := &fakeCloser{}
	e := risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: ks,
		Settings:   system.NewRuntimeSettingsRepository(db),
		Positions:  trading.NewPositionRepository(db),
		Orders:     orders,
		Closer:     closer,
		Now:        func() time.Time { return clock },
	})
	ctx := context.Background()

	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	insertOrphan := func(filledAt time.Time) {
		t.Helper()
		o, err := orders.Insert(ctx, domain.PaperOrder{
			InstrumentID: inst.ID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
			Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: filledAt,
		})
		if err != nil {
			t.Fatalf("insert order: %v", err)
		}
		if _, err := orders.Fill(ctx, o.ID, 2100, 0, nil, filledAt); err != nil {
			t.Fatalf("fill order: %v", err)
		}
	}

	insertOrphan(clock.Add(-5 * time.Minute))
	if err := e.CheckPositionReconciliation(ctx); err != nil {
		t.Fatalf("CheckPositionReconciliation: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || got[0] != domain.KillReasonFillDiscrepancy {
		t.Fatalf("unresolved = %v, want [fill_discrepancy]", got)
	}

	clock = clock.Add(2 * time.Minute)
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := e.CheckPositionReconciliation(ctx); err != nil {
		t.Fatalf("CheckPositionReconciliation after Resume: %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 0 {
		t.Fatalf("unresolved after Resume+check = %v, want none (no re-fire)", got)
	}
	if len(closer.closed) != 1 {
		t.Fatalf("closer.closed = %v, want only the original flatten", closer.closed)
	}

	// A new orphan filled after the Resume still fires.
	clock = clock.Add(5 * time.Minute)
	insertOrphan(clock.Add(-3 * time.Minute))
	if err := e.CheckPositionReconciliation(ctx); err != nil {
		t.Fatalf("CheckPositionReconciliation (new orphan): %v", err)
	}
	if got := unresolvedReasons(t, ks); len(got) != 1 || got[0] != domain.KillReasonFillDiscrepancy {
		t.Fatalf("unresolved = %v, want a new fill_discrepancy", got)
	}
}
