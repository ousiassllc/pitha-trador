package repoportfolio_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/repoportfolio"
)

const portfolioTestCapital = 1_000_000

// portfolioTestNow is midday JST on a fixed day, so "today" is stable.
var portfolioTestNow = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

// newPortfolioTestFixtures returns a RepositoryPortfolioProvider plus the
// PositionRepository/OrderRepository/instrumentID it is backed by,
// mirroring internal/repository/trading/position_repo_test.go's own
// openTestPositionRepo/insertFilledEntryOrder helpers (unexported there,
// so not reusable from this package).
func newPortfolioTestFixtures(t *testing.T) (*repoportfolio.Provider, *trading.PositionRepository, *trading.OrderRepository, int64) {
	t.Helper()
	db := newTestDB(t)

	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	positions := trading.NewPositionRepository(db)
	orders := trading.NewOrderRepository(db)
	return repoportfolio.New(positions, portfolioTestCapital, repoportfolio.WithClock(func() time.Time { return portfolioTestNow })), positions, orders, inst.ID
}

func mustOpenPosition(t *testing.T, positions *trading.PositionRepository, orders *trading.OrderRepository, instrumentID int64, now time.Time) domain.Position {
	t.Helper()
	ctx := context.Background()
	entry, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert entry order: %v", err)
	}
	opened, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return opened
}

func mustClosePosition(t *testing.T, positions *trading.PositionRepository, orders *trading.OrderRepository, opened domain.Position, realizedPnL float64, closedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	exit, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: opened.InstrumentID, Symbol: opened.Symbol, Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: opened.Quantity, Status: domain.OrderStatusFilled, SubmittedAt: closedAt,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}
	exitPrice := opened.EntryPrice
	if realizedPnL < 0 {
		exitPrice -= 10
	} else if realizedPnL > 0 {
		exitPrice += 10
	}
	if _, err := positions.Close(ctx, opened.ID, exit.ID, exitPrice, realizedPnL, domain.ExitReasonManual, closedAt); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRepositoryPortfolioProvider_OpenPositionCount_CountsOnlyOpenPositions(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if count, err := provider.OpenPositionCount(ctx); err != nil || count != 0 {
		t.Fatalf("OpenPositionCount (none opened) = (%d, %v), want (0, nil)", count, err)
	}

	opened := mustOpenPosition(t, positions, orders, instrumentID, now)
	if count, err := provider.OpenPositionCount(ctx); err != nil || count != 1 {
		t.Fatalf("OpenPositionCount (one opened) = (%d, %v), want (1, nil)", count, err)
	}

	mustClosePosition(t, positions, orders, opened, 500, now.Add(time.Minute))
	if count, err := provider.OpenPositionCount(ctx); err != nil || count != 0 {
		t.Fatalf("OpenPositionCount (after close) = (%d, %v), want (0, nil)", count, err)
	}
}

func TestRepositoryPortfolioProvider_OpenPositionCountBySide_CountsOnlyOpenPositionsOnThatSide(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()

	closed := mustOpenPosition(t, positions, orders, instrumentID, now)
	mustClosePosition(t, positions, orders, closed, 500, now.Add(time.Minute))
	mustOpenPosition(t, positions, orders, instrumentID, now.Add(2*time.Minute))

	for side, want := range map[string]int{domain.PositionSideLong: 1, domain.PositionSideShort: 0} {
		if got, err := provider.OpenPositionCountBySide(ctx, side); err != nil || got != want {
			t.Errorf("OpenPositionCountBySide(%s) = (%d, %v), want (%d, nil)", side, got, err, want)
		}
	}
}

func TestRepositoryPortfolioProvider_ConsecutiveLosses_CountsFromMostRecentClose(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	now := time.Now().UTC()

	if count, err := provider.ConsecutiveLosses(context.Background(), time.Time{}); err != nil || count != 0 {
		t.Fatalf("ConsecutiveLosses (no history) = (%d, %v), want (0, nil)", count, err)
	}

	// win, then two losses: ConsecutiveLosses only counts the losing
	// streak since the most recent close, stopping at the earlier win.
	p1 := mustOpenPosition(t, positions, orders, instrumentID, now)
	mustClosePosition(t, positions, orders, p1, 300, now.Add(time.Minute))

	p2 := mustOpenPosition(t, positions, orders, instrumentID, now.Add(2*time.Minute))
	mustClosePosition(t, positions, orders, p2, -100, now.Add(3*time.Minute))

	p3 := mustOpenPosition(t, positions, orders, instrumentID, now.Add(4*time.Minute))
	mustClosePosition(t, positions, orders, p3, -200, now.Add(5*time.Minute))

	count, err := provider.ConsecutiveLosses(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("ConsecutiveLosses: %v", err)
	}
	if count != 2 {
		t.Errorf("ConsecutiveLosses = %d, want 2", count)
	}
}

func TestRepositoryPortfolioProvider_LastLossAt_ReturnsZeroTimeWhenNeverLost(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	now := time.Now().UTC()

	if at, err := provider.LastLossAt(context.Background(), time.Time{}); err != nil || !at.IsZero() {
		t.Fatalf("LastLossAt (no history) = (%v, %v), want (zero time, nil)", at, err)
	}

	opened := mustOpenPosition(t, positions, orders, instrumentID, now)
	closedAt := now.Add(time.Minute)
	mustClosePosition(t, positions, orders, opened, -400, closedAt)

	at, err := provider.LastLossAt(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("LastLossAt: %v", err)
	}
	if !at.Equal(closedAt) {
		t.Errorf("LastLossAt = %v, want %v", at, closedAt)
	}
}

func TestRepositoryPortfolioProvider_Exposure_MeasuresOpenNotionalAgainstCapital(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	ctx := context.Background()

	mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow) // 100 x 2100 = 210,000 of 1,000,000

	total, err := provider.TotalExposurePct(ctx)
	if err != nil || math.Abs(total-21) > 1e-9 {
		t.Fatalf("TotalExposurePct = (%v, %v), want (21, nil)", total, err)
	}
	same, err := provider.SymbolExposurePct(ctx, instrumentID)
	if err != nil || math.Abs(same-21) > 1e-9 {
		t.Fatalf("SymbolExposurePct(held) = (%v, %v), want (21, nil)", same, err)
	}
	other, err := provider.SymbolExposurePct(ctx, instrumentID+1)
	if err != nil || other != 0 {
		t.Fatalf("SymbolExposurePct(other) = (%v, %v), want (0, nil)", other, err)
	}
}

func TestRepositoryPortfolioProvider_DailyLossPct_CountsTodaysRealizedAndUnrealizedLoss(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	ctx := context.Background()

	// A loss closed yesterday (JST) must not count; today's -10,000 does.
	yesterday := mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow.Add(-26*time.Hour))
	mustClosePosition(t, positions, orders, yesterday, -50_000, portfolioTestNow.Add(-25*time.Hour))
	today := mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow.Add(-2*time.Hour))
	mustClosePosition(t, positions, orders, today, -10_000, portfolioTestNow.Add(-time.Hour))

	got, err := provider.DailyLossPct(ctx, time.Time{})
	if err != nil || math.Abs(got-1) > 1e-9 {
		t.Fatalf("DailyLossPct = (%v, %v), want (1, nil): only today's -10,000 of 1,000,000", got, err)
	}

	// An open position marked down 5,000 adds unrealized loss.
	open := mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow)
	if _, err := positions.Mark(ctx, open.ID, open.EntryPrice-50, -5_000, portfolioTestNow); err != nil {
		t.Fatalf("UpdateMark: %v", err)
	}
	got, err = provider.DailyLossPct(ctx, time.Time{})
	if err != nil || math.Abs(got-1.5) > 1e-9 {
		t.Fatalf("DailyLossPct with unrealized = (%v, %v), want (1.5, nil)", got, err)
	}
}

func TestRepositoryPortfolioProvider_NoInitialCapital_FailsInsteadOfReportingZero(t *testing.T) {
	_, positions, _, instrumentID := newPortfolioTestFixtures(t)
	provider := repoportfolio.New(positions, 0)
	ctx := context.Background()

	if _, err := provider.TotalExposurePct(ctx); !errors.Is(err, repoportfolio.ErrNoInitialCapital) {
		t.Errorf("TotalExposurePct err = %v, want ErrNoInitialCapital", err)
	}
	if _, err := provider.SymbolExposurePct(ctx, instrumentID); !errors.Is(err, repoportfolio.ErrNoInitialCapital) {
		t.Errorf("SymbolExposurePct err = %v, want ErrNoInitialCapital", err)
	}
	if _, err := provider.DailyLossPct(ctx, time.Time{}); !errors.Is(err, repoportfolio.ErrNoInitialCapital) {
		t.Errorf("DailyLossPct err = %v, want ErrNoInitialCapital", err)
	}
}

// The manual-resume baseline (#165/#172): losses closed at or before it must
// not count toward the streak, the cooldown gate or the day's realized loss.
func TestRepositoryPortfolioProvider_Since_IgnoresPositionsClosedBeforeBaseline(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	ctx := context.Background()

	for i, pnl := range []float64{-100, -200} {
		opened := mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow.Add(time.Duration(-40+i)*time.Minute))
		mustClosePosition(t, positions, orders, opened, pnl, portfolioTestNow.Add(time.Duration(-30+i)*time.Minute))
	}
	baseline := portfolioTestNow.Add(-10 * time.Minute)

	if n, err := provider.ConsecutiveLosses(ctx, baseline); err != nil || n != 0 {
		t.Fatalf("ConsecutiveLosses(since baseline) = (%d, %v), want (0, nil)", n, err)
	}
	if at, err := provider.LastLossAt(ctx, baseline); err != nil || !at.IsZero() {
		t.Fatalf("LastLossAt(since baseline) = (%v, %v), want (zero, nil)", at, err)
	}
	if got, err := provider.DailyLossPct(ctx, baseline); err != nil || got != 0 {
		t.Fatalf("DailyLossPct(since baseline) = (%v, %v), want (0, nil)", got, err)
	}

	// A loss after the baseline counts again, alone.
	opened := mustOpenPosition(t, positions, orders, instrumentID, portfolioTestNow.Add(-8*time.Minute))
	closedAt := portfolioTestNow.Add(-5 * time.Minute)
	mustClosePosition(t, positions, orders, opened, -1000, closedAt)
	if n, err := provider.ConsecutiveLosses(ctx, baseline); err != nil || n != 1 {
		t.Fatalf("ConsecutiveLosses after new loss = (%d, %v), want (1, nil)", n, err)
	}
	if at, err := provider.LastLossAt(ctx, baseline); err != nil || !at.Equal(closedAt) {
		t.Fatalf("LastLossAt after new loss = (%v, %v), want %v", at, err, closedAt)
	}
	if got, err := provider.DailyLossPct(ctx, baseline); err != nil || math.Abs(got-0.1) > 1e-9 {
		t.Fatalf("DailyLossPct after new loss = (%v, %v), want (0.1, nil)", got, err)
	}
}
