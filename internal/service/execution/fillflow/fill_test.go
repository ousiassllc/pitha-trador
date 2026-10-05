package fillflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// costEngine is an Engine under the TSE calendar and a model with every
// cost switched on: 1bp slippage, 10bps fees.
func costEngine(t *testing.T) testEngine {
	cfg := execution.DefaultConfig()
	cfg.Calendar = marketcalendar.TSE
	cfg.Fill = fillmodel.Model{SlippageBps: 1, AuctionSlippageBps: 100, FeeBps: 10}
	return newTestEngine(t, cfg)
}

func enterWithBook(t *testing.T, te testEngine, at time.Time, book fillmodel.Book) execution.EntryResult {
	t.Helper()
	result, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Book: book, Now: at,
	})
	if err != nil {
		t.Fatalf("Enter at %s: %v", at.Format("15:04"), err)
	}
	return result
}

// TestEnter_FillsAtTheAskWithSlippageFeesAndTicks: a market buy is not
// filled at the signal price 2000 but at ask 2001 + 1bp slippage rounded up
// on the tick grid, with the fee and slippage recorded on the order.
func TestEnter_FillsAtTheAskWithSlippageFeesAndTicks(t *testing.T) {
	te := costEngine(t)
	result := enterWithBook(t, te, jstAt(29, 10, 0), fillmodel.Book{Bid: 1999, Ask: 2001})

	// 2001 * 1.0001 = 2001.2 -> 2002
	if got := result.Position.EntryPrice; got != 2002 {
		t.Fatalf("EntryPrice = %v, want 2002 (ask + slippage on the tick grid)", got)
	}
	order := result.Order
	if order.FilledPrice == nil || *order.FilledPrice != 2002 {
		t.Fatalf("order.FilledPrice = %v, want 2002", order.FilledPrice)
	}
	if want := 2002.0 * 100 * 10 / 10000; order.Fees != want {
		t.Errorf("order.Fees = %v, want %v (10bps of the notional)", order.Fees, want)
	}
	if order.SlippageBps == nil || *order.SlippageBps <= 0 {
		t.Errorf("order.SlippageBps = %v, want positive (adverse versus the signal price)", order.SlippageBps)
	}
}

// TestClose_FillsAtTheBidAndRealizedPnLIsNetOfBothFees: the exit fills
// below the signal price at the bid and realized P&L deducts both fills'
// fees.
func TestClose_FillsAtTheBidAndRealizedPnLIsNetOfBothFees(t *testing.T) {
	te := costEngine(t)
	ctx := context.Background()
	entry := enterWithBook(t, te, jstAt(29, 10, 0), fillmodel.Book{}) // 2000 * 1.0001 = 2000.2 -> 2001

	closed, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonManual, 2010, fillmodel.Book{Bid: 2008, Ask: 2012}, jstAt(29, 10, 5))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	exitOrder, err := te.orders.Get(ctx, *closed.ExitOrderID)
	if err != nil {
		t.Fatalf("Get exit order: %v", err)
	}
	// 2008 * 0.9999 = 2007.8 -> 2007
	if exitOrder.FilledPrice == nil || *exitOrder.FilledPrice != 2007 {
		t.Fatalf("exit FilledPrice = %v, want 2007 (bid - slippage, rounded down)", exitOrder.FilledPrice)
	}
	if exitOrder.Side != domain.OrderSideSell {
		t.Fatalf("exit side = %q, want SELL", exitOrder.Side)
	}
	entryFee, exitFee := 2001.0*100*10/10000, 2007.0*100*10/10000
	if exitOrder.Fees != exitFee {
		t.Errorf("exit Fees = %v, want %v", exitOrder.Fees, exitFee)
	}
	if want := (2007-2001)*100 - entryFee - exitFee; closed.RealizedPnL == nil || *closed.RealizedPnL != want {
		t.Fatalf("RealizedPnL = %v, want %v ((exit - entry) * qty - both fees)", closed.RealizedPnL, want)
	}
}

// TestEnter_OpeningAuctionIsASeparateFill: the 9:00 寄り crosses at the
// indicative price with the auction slippage and no spread, while the same
// order at 10:00 crosses the spread.
func TestEnter_OpeningAuctionIsASeparateFill(t *testing.T) {
	book := fillmodel.Book{Bid: 1990, Ask: 2010}
	auction := enterWithBook(t, costEngine(t), jstAt(29, 9, 0), book)
	if got := auction.Position.EntryPrice; got != 2020 { // 2000 * 1.01
		t.Errorf("寄り EntryPrice = %v, want 2020 (indicative + auction slippage, no spread)", got)
	}
	continuous := enterWithBook(t, costEngine(t), jstAt(29, 10, 0), book)
	if got := continuous.Position.EntryPrice; got != 2011 { // 2010 * 1.0001 = 2010.2 -> 2011
		t.Errorf("ザラ場 EntryPrice = %v, want 2011 (ask + slippage)", got)
	}
}

// TestLunchBreak: nothing fills 11:30-12:30. A manual Close is refused and
// leaves the position open, an exit triggering on a lunch snapshot waits for
// the next in-session bar, and a PENDING limit entry is not filled.
func TestLunchBreak(t *testing.T) {
	ctx := context.Background()

	t.Run("Close is refused", func(t *testing.T) {
		te := costEngine(t)
		entry := enterWithBook(t, te, jstAt(29, 11, 0), fillmodel.Book{})
		_, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonManual, 2000, fillmodel.Book{}, jstAt(29, 12, 0))
		if !errors.Is(err, execution.ErrOutsideTradingSession) {
			t.Fatalf("Close err = %v, want ErrOutsideTradingSession", err)
		}
		if p, err := te.positions.Get(ctx, entry.Position.ID); err != nil || !p.IsOpen() {
			t.Fatalf("position after refused Close = %+v, %v; want still open", p, err)
		}
		if _, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonManual, 2000, fillmodel.Book{}, jstAt(29, 12, 30)); err != nil {
			t.Fatalf("Close at the 後場寄り: %v", err)
		}
	})

	t.Run("stop loss waits for 後場", func(t *testing.T) {
		te := costEngine(t)
		entry := enterWithBook(t, te, jstAt(29, 11, 0), fillmodel.Book{})
		crashed := snapshotAt(te.instrument.ID, 1900, jstAt(29, 12, 0)) // -5%
		result, err := te.engine.OnSnapshot(ctx, crashed)
		if err != nil || result.Exited || result.Position == nil {
			t.Fatalf("OnSnapshot at 12:00 = %+v, %v; want the position held", result, err)
		}
		crashed.Timestamp = jstAt(29, 12, 31)
		result, err = te.engine.OnSnapshot(ctx, crashed)
		if err != nil || !result.Exited {
			t.Fatalf("OnSnapshot at 12:31 = %+v, %v; want the stop loss to fill", result, err)
		}
		closed, _ := te.positions.Get(ctx, entry.Position.ID)
		if closed.ExitOrderID == nil {
			t.Fatal("closed position has no exit order")
		}
	})

	t.Run("pending limit does not fill", func(t *testing.T) {
		te := costEngine(t)
		limit := 2000.0
		result, err := te.engine.Enter(ctx, execution.EntryRequest{
			Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeLimit,
			LimitPrice: &limit, Price: 2100, Now: jstAt(29, 11, 0),
		})
		if err != nil || result.Position != nil {
			t.Fatalf("Enter = %+v, %v; want a PENDING order", result, err)
		}
		_, ok, err := te.engine.TryFillPending(ctx, result.Order.ID, domain.JevDirectionLong, 1990, fillmodel.Book{}, jstAt(29, 12, 0))
		if err != nil || ok {
			t.Fatalf("TryFillPending at lunch = ok %v, err %v; want no fill", ok, err)
		}
		_, ok, err = te.engine.TryFillPending(ctx, result.Order.ID, domain.JevDirectionLong, 1990, fillmodel.Book{}, jstAt(29, 12, 31))
		if err != nil || !ok {
			t.Fatalf("TryFillPending at 12:31 = ok %v, err %v; want a fill", ok, err)
		}
	})
}

// TestEnter_LimitOrderWaitsForTheTouch: a buy limit at 2000 does not fill
// while the ask is 2001 even though the last price is 2000.
func TestEnter_LimitOrderWaitsForTheTouch(t *testing.T) {
	te := costEngine(t)
	limit := 2000.0
	result, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeLimit,
		LimitPrice: &limit, Price: 2000, Book: fillmodel.Book{Bid: 1999, Ask: 2001}, Now: jstAt(29, 10, 0),
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if result.Position != nil || result.Order.Status != domain.OrderStatusPending {
		t.Fatalf("Enter = %+v, want a PENDING order (the ask 2001 is above the limit)", result)
	}
}
