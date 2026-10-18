package trading_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestPositionRepository_ListOpenedBetweenReturnsOpenAndClosedInRange(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	// open opens a position at base+offset; closed ones are closed at once so
	// the instrument's one-open-position rule lets the next one open.
	open := func(offset time.Duration, closed bool) domain.Position {
		at := base.Add(offset)
		entry := insertFilledEntryOrder(t, orders, instrumentID, at)
		p, err := positions.Open(ctx, domain.Position{
			InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203",
			Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: at,
		})
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if closed {
			exit, err := orders.Insert(ctx, domain.PaperOrder{
				InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideSell,
				OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: at,
			})
			if err != nil {
				t.Fatalf("insert exit order: %v", err)
			}
			if p, err = positions.Close(ctx, p.ID, exit.ID, 2100, 0, domain.ExitReasonStopLoss, at.Add(time.Minute)); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}
		return p
	}

	before := open(0, true)
	inClosed := open(24*time.Hour, true)
	inOpen := open(25*time.Hour, false)

	got, err := positions.ListOpenedBetween(ctx, base.Add(12*time.Hour), base.Add(36*time.Hour))
	if err != nil {
		t.Fatalf("ListOpenedBetween: %v", err)
	}
	if len(got) != 2 || got[0].ID != inClosed.ID || got[1].ID != inOpen.ID {
		t.Fatalf("ListOpenedBetween = %+v, want the closed then the open in-range position (not %d)", got, before.ID)
	}
}
