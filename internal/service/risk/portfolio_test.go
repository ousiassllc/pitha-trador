package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// newPortfolioTestFixtures returns a RepositoryPortfolioProvider plus the
// PositionRepository/OrderRepository/instrumentID it is backed by,
// mirroring internal/repository/position_repo_test.go's own
// openTestPositionRepo/insertFilledEntryOrder helpers (unexported there,
// so not reusable from this package).
func newPortfolioTestFixtures(t *testing.T) (*risk.RepositoryPortfolioProvider, *repository.PositionRepository, *repository.OrderRepository, int64) {
	t.Helper()
	db := newTestDB(t)

	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	positions := repository.NewPositionRepository(db)
	orders := repository.NewOrderRepository(db)
	return risk.NewRepositoryPortfolioProvider(positions), positions, orders, inst.ID
}

func mustOpenPosition(t *testing.T, positions *repository.PositionRepository, orders *repository.OrderRepository, instrumentID int64, now time.Time) domain.Position {
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

func mustClosePosition(t *testing.T, positions *repository.PositionRepository, orders *repository.OrderRepository, opened domain.Position, realizedPnL float64, closedAt time.Time) {
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

func TestRepositoryPortfolioProvider_ConsecutiveLosses_CountsFromMostRecentClose(t *testing.T) {
	provider, positions, orders, instrumentID := newPortfolioTestFixtures(t)
	now := time.Now().UTC()

	if count, err := provider.ConsecutiveLosses(context.Background()); err != nil || count != 0 {
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

	count, err := provider.ConsecutiveLosses(context.Background())
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

	if at, err := provider.LastLossAt(context.Background()); err != nil || !at.IsZero() {
		t.Fatalf("LastLossAt (no history) = (%v, %v), want (zero time, nil)", at, err)
	}

	opened := mustOpenPosition(t, positions, orders, instrumentID, now)
	closedAt := now.Add(time.Minute)
	mustClosePosition(t, positions, orders, opened, -400, closedAt)

	at, err := provider.LastLossAt(context.Background())
	if err != nil {
		t.Fatalf("LastLossAt: %v", err)
	}
	if !at.Equal(closedAt) {
		t.Errorf("LastLossAt = %v, want %v", at, closedAt)
	}
}
