package trading_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// A closed position holds no unrealized P&L (huma-api.md `GET /positions`):
// the last Mark value (+850 here) must be reset to 0 by Close and
// CloseWithExitOrder, so closed rows never show unrealized and realized P&L
// side by side (issue #682).
func TestPositionRepository_Close_ResetsUnrealizedPnL(t *testing.T) {
	for _, how := range []string{"Close", "CloseWithExitOrder"} {
		t.Run(how, func(t *testing.T) {
			positions, orders, instrumentID := openTestPositionRepo(t)
			ctx := context.Background()
			now := time.Now().UTC()
			entry := insertFilledEntryOrder(t, orders, instrumentID, now)

			opened, err := positions.Open(ctx, domain.Position{
				InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203",
				Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
			})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			marked, err := positions.Mark(ctx, opened.ID, 2108.5, 850.0, now.Add(time.Minute))
			if err != nil || marked.UnrealizedPnL != 850.0 {
				t.Fatalf("Mark() = (%+v, %v), want UnrealizedPnL=850.0", marked, err)
			}

			exitOrder := domain.PaperOrder{
				InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideSell,
				OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
			}
			closedAt := now.Add(5 * time.Minute)
			var closed domain.Position
			if how == "Close" {
				exitOrder.Status = domain.OrderStatusFilled
				exit, err := orders.Insert(ctx, exitOrder)
				if err != nil {
					t.Fatalf("insert exit order: %v", err)
				}
				closed, err = positions.Close(ctx, opened.ID, exit.ID, 2105.0, 500.0, domain.ExitReasonManual, closedAt)
				if err != nil {
					t.Fatalf("Close: %v", err)
				}
			} else {
				closed, err = positions.CloseWithExitOrder(ctx, exitOrder, 2105.0, opened.ID, 500.0, domain.ExitReasonManual, closedAt)
				if err != nil {
					t.Fatalf("CloseWithExitOrder: %v", err)
				}
			}
			if closed.UnrealizedPnL != 0 {
				t.Fatalf("%s() UnrealizedPnL = %v, want 0", how, closed.UnrealizedPnL)
			}
			stored, err := positions.Get(ctx, opened.ID)
			if err != nil || stored.UnrealizedPnL != 0 {
				t.Fatalf("Get() after %s = (%+v, %v), want UnrealizedPnL=0", how, stored, err)
			}
		})
	}
}
