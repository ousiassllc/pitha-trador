package trading_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

// ListClosedReturns returns realized_pnl over the entry notional in bps, with
// the exit reason, for the positions closed in [from, to) only.
func TestPositionRepository_ListClosedReturns(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	positions, orders := trading.NewPositionRepository(db), trading.NewOrderRepository(db)
	t0 := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)

	closeAt := func(symbol string, closedAt time.Time, pnl float64, reason string) {
		t.Helper()
		instID := insertInstrument(t, db, symbol, symbol)
		entry := insertFilledEntryOrder(t, orders, instID, t0)
		opened, err := positions.Open(ctx, domain.Position{InstrumentID: instID, EntryOrderID: entry.ID, Symbol: symbol, Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2000, CurrentPrice: 2000, OpenedAt: t0})
		if err != nil {
			t.Fatalf("Open %s: %v", symbol, err)
		}
		if _, err := positions.Close(ctx, opened.ID, entry.ID, 2000, pnl, reason, closedAt); err != nil {
			t.Fatalf("Close %s: %v", symbol, err)
		}
	}
	closeAt("1001", t0.Add(10*time.Minute), 500, domain.ExitReasonTakeProfit) // +25 bps of the 200,000 notional
	closeAt("1002", t0.Add(20*time.Minute), -300, domain.ExitReasonStopLoss)  // -15 bps
	closeAt("1003", t0.Add(2*time.Hour), 9999, domain.ExitReasonMaxHolding)   // outside the window

	// An open position is never listed.
	instID := insertInstrument(t, db, "1004", "1004")
	entry := insertFilledEntryOrder(t, orders, instID, t0)
	if _, err := positions.Open(ctx, domain.Position{InstrumentID: instID, EntryOrderID: entry.ID, Symbol: "1004", Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2000, CurrentPrice: 2000, OpenedAt: t0}); err != nil {
		t.Fatalf("Open open position: %v", err)
	}

	got, err := positions.ListClosedReturns(ctx, t0, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("ListClosedReturns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want the two positions closed in the window", got)
	}
	if got[0].ExitReason != domain.ExitReasonTakeProfit || math.Abs(got[0].ReturnBps-25) > 1e-9 {
		t.Errorf("got[0] = %+v, want take_profit at +25 bps", got[0])
	}
	if got[1].ExitReason != domain.ExitReasonStopLoss || math.Abs(got[1].ReturnBps+15) > 1e-9 {
		t.Errorf("got[1] = %+v, want stop_loss at -15 bps", got[1])
	}
	none, err := positions.ListClosedReturns(ctx, t0.Add(24*time.Hour), t0.Add(48*time.Hour))
	if err != nil || len(none) != 0 {
		t.Errorf("empty window = %+v, %v", none, err)
	}
}
