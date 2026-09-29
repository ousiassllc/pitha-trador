package risk_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

func TestEngine_Check_PassesWhenNoLimitBreachedAndSystemRunning(t *testing.T) {
	e, _ := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if !passed || reason != "" {
		t.Fatalf("Check = (%v, %q), want (true, \"\")", passed, reason)
	}
}

func TestEngine_Check_RejectsWhenKilled(t *testing.T) {
	e, _ := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)
	ctx := context.Background()
	if err := e.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)

	if passed {
		t.Fatalf("Check: passed = true, want false")
	}
	if !strings.HasPrefix(reason, risk.ReasonKillSwitchActive) {
		t.Fatalf("Check: reason = %q, want prefix %q", reason, risk.ReasonKillSwitchActive)
	}
}

func TestEngine_Check_RejectsWhenPaused(t *testing.T) {
	e, _ := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)
	ctx := context.Background()
	if err := e.Pause(ctx); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)

	if passed || reason != risk.ReasonSystemPaused {
		t.Fatalf("Check = (%v, %q), want (false, %q)", passed, reason, risk.ReasonSystemPaused)
	}
}

func TestEngine_Check_RejectsOnMaxOpenPositions(t *testing.T) {
	limits := testLimits()
	e, _ := newEngine(t, limits, fakePortfolio{openPositionCount: limits.MaxOpenPositions}, nil, nil)

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if passed || !strings.HasPrefix(reason, risk.ReasonMaxOpenPositions) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxOpenPositions)
	}
}

func TestEngine_Check_RejectsOnMaxTotalExposurePct(t *testing.T) {
	limits := testLimits()
	e, _ := newEngine(t, limits, fakePortfolio{totalExposurePct: limits.MaxTotalExposurePct}, nil, nil)

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if passed || !strings.HasPrefix(reason, risk.ReasonMaxTotalExposurePct) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxTotalExposurePct)
	}
}

func TestEngine_Check_RejectsOnMaxPositionPerSymbolPct(t *testing.T) {
	limits := testLimits()
	e, _ := newEngine(t, limits, fakePortfolio{symbolExposurePct: limits.MaxPositionPerSymbolPct}, nil, nil)

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if passed || !strings.HasPrefix(reason, risk.ReasonMaxPositionPerSymbolPct) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxPositionPerSymbolPct)
	}
}

func TestEngine_Check_RejectsOnCooldownAfterLoss(t *testing.T) {
	limits := testLimits()
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	lastLoss := now.Add(-1 * time.Minute) // within the 5-minute cooldown
	e, _ := newEngine(t, limits, fakePortfolio{lastLossAt: lastLoss}, nil, func() time.Time { return now })

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if passed || !strings.HasPrefix(reason, risk.ReasonCooldownAfterLoss) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonCooldownAfterLoss)
	}
}

func TestEngine_Check_AllowsAfterCooldownElapsed(t *testing.T) {
	limits := testLimits()
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	lastLoss := now.Add(-10 * time.Minute) // past the 5-minute cooldown
	e, _ := newEngine(t, limits, fakePortfolio{lastLossAt: lastLoss}, nil, func() time.Time { return now })

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if !passed || reason != "" {
		t.Fatalf("Check = (%v, %q), want (true, \"\")", passed, reason)
	}
}

func TestEngine_Check_MaxDailyLossPct_RejectsAndTriggersKillSwitchOnce(t *testing.T) {
	limits := testLimits()
	closer := &fakeCloser{}
	e, killSwitch := newEngine(t, limits, fakePortfolio{dailyLossPct: limits.MaxDailyLossPct}, closer, nil)
	ctx := context.Background()

	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonMaxDailyLossPct) {
		t.Fatalf("first Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxDailyLossPct)
	}

	// Kill Switch is now active: subsequent candidates are rejected via
	// the Kill Switch check, and daily_loss_limit force-closes positions
	// (docs/architecture/overview.md §10.3).
	passed, reason = e.Check(ctx, 2, domain.JevDirectionShort)
	if passed || !strings.HasPrefix(reason, risk.ReasonKillSwitchActive) {
		t.Fatalf("second Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonKillSwitchActive)
	}

	events, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(events) != 1 || events[0].Reason != domain.KillReasonDailyLossLimit {
		t.Fatalf("ListUnresolved = %+v, want exactly one daily_loss_limit event (idempotent trigger)", events)
	}
	if len(closer.closed) != 1 || closer.closed[0] != domain.KillReasonDailyLossLimit {
		t.Fatalf("closer.closed = %v, want [%q] (FR-RISK-3 force-close)", closer.closed, domain.KillReasonDailyLossLimit)
	}
}

func TestEngine_Check_MaxConsecutiveLosses_RejectsAndTriggersKillSwitch(t *testing.T) {
	limits := testLimits()
	e, killSwitch := newEngine(t, limits, fakePortfolio{consecutiveLosses: limits.MaxConsecutiveLosses}, nil, nil)
	ctx := context.Background()

	passed, reason := e.Check(ctx, 1, domain.JevDirectionLong)
	if passed || !strings.HasPrefix(reason, risk.ReasonMaxConsecutiveLosses) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxConsecutiveLosses)
	}

	events, err := killSwitch.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(events) != 1 || events[0].Reason != domain.KillReasonConsecutiveLosses {
		t.Fatalf("ListUnresolved = %+v, want exactly one consecutive_losses event", events)
	}
}

func TestEngine_Check_RejectsOnMaxSpreadBps(t *testing.T) {
	limits := testLimits()
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}

	snapshots := repository.NewSnapshotRepository(db)
	wideSpread := limits.MaxSpreadBps + 1
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID,
		Timestamp:    time.Now(),
		SpreadBps:    &wideSpread,
		RawDataJSON:  "{}",
	}); err != nil {
		t.Fatalf("Insert snapshot: %v", err)
	}

	e := risk.NewEngine(risk.Config{
		Limits:     limits,
		KillSwitch: repository.NewKillSwitchRepository(db),
		Settings:   repository.NewRuntimeSettingsRepository(db),
		Snapshots:  snapshots,
		Portfolio:  risk.ZeroPortfolioProvider{},
	})

	passed, reason := e.Check(context.Background(), inst.ID, domain.JevDirectionLong)

	if passed || !strings.HasPrefix(reason, risk.ReasonMaxSpreadBps) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxSpreadBps)
	}
}

var errPortfolioDown = errors.New("database is locked")

// erroring wraps a healthy portfolio and fails exactly one method, so each
// portfolio read is proven to fail closed on its own.
type erroring struct {
	fakePortfolio
	failing string
}

func (e erroring) fail(name string) error {
	if e.failing == name {
		return errPortfolioDown
	}
	return nil
}
func (e erroring) OpenPositionCount(ctx context.Context) (int, error) {
	return 0, e.fail("OpenPositionCount")
}
func (e erroring) TotalExposurePct(ctx context.Context) (float64, error) {
	return 0, e.fail("TotalExposurePct")
}
func (e erroring) SymbolExposurePct(ctx context.Context, _ int64) (float64, error) {
	return 0, e.fail("SymbolExposurePct")
}
func (e erroring) DailyLossPct(ctx context.Context) (float64, error) {
	return 0, e.fail("DailyLossPct")
}
func (e erroring) ConsecutiveLosses(ctx context.Context) (int, error) {
	return 0, e.fail("ConsecutiveLosses")
}
func (e erroring) LastLossAt(ctx context.Context) (time.Time, error) {
	return time.Time{}, e.fail("LastLossAt")
}

func TestEngine_Check_FailsClosedWhenAnyPortfolioReadErrors(t *testing.T) {
	for _, method := range []string{
		"OpenPositionCount", "TotalExposurePct", "SymbolExposurePct",
		"DailyLossPct", "ConsecutiveLosses", "LastLossAt",
	} {
		t.Run(method, func(t *testing.T) {
			e, _ := newEngine(t, testLimits(), erroring{failing: method}, nil, nil)

			passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

			if passed || !strings.HasPrefix(reason, risk.ReasonRiskEngineError) {
				t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonRiskEngineError)
			}
		})
	}
}

func TestEngine_Check_FailsClosedOnMissingSnapshotOrSpread(t *testing.T) {
	// No snapshot row for the instrument at all.
	e, _ := newEngine(t, testLimits(), risk.ZeroPortfolioProvider{}, nil, nil)
	if passed, reason := e.Check(context.Background(), 999, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonRiskEngineError) {
		t.Errorf("Check(no snapshot) = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonRiskEngineError)
	}

	// Latest snapshot has a NULL spread (FR-POLICY-3 データ欠損).
	db := newTestDB(t)
	snapshots := repository.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 2000, -1)
	e = risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: repository.NewKillSwitchRepository(db),
		Settings:   repository.NewRuntimeSettingsRepository(db),
		Snapshots:  snapshots,
		Portfolio:  risk.ZeroPortfolioProvider{},
	})
	if passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonRiskEngineError) {
		t.Errorf("Check(NULL spread) = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonRiskEngineError)
	}
}

func TestEngine_Check_RejectsWhenNoLotFitsMaxTradeLoss(t *testing.T) {
	limits := testLimits()
	// 0.25% of 30M = 75,000 max loss; stop 0.6% of a 300,000-yen share is
	// 1,800 yen/share -> 41 shares < one 100-share lot. The symbol/total
	// caps are widened so max_trade_loss_pct is the binding limit.
	limits.MaxPositionPerSymbolPct = 100
	limits.MaxTotalExposurePct = 100
	db := newTestDB(t)
	snapshots := repository.NewSnapshotRepository(db)
	seedSnapshot(t, db, snapshots, 300_000, 5)
	e := risk.NewEngine(risk.Config{
		Limits:     limits,
		KillSwitch: repository.NewKillSwitchRepository(db),
		Settings:   repository.NewRuntimeSettingsRepository(db),
		Snapshots:  snapshots,
		Portfolio:  risk.ZeroPortfolioProvider{},
	})

	passed, reason := e.Check(context.Background(), 1, domain.JevDirectionLong)

	if passed || !strings.HasPrefix(reason, risk.ReasonMaxTradeLossPct) {
		t.Fatalf("Check = (%v, %q), want (false, prefix %q)", passed, reason, risk.ReasonMaxTradeLossPct)
	}
}
