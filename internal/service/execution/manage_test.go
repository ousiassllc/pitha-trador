package execution_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func snapshotAt(instrumentID int64, price float64, at time.Time) domain.Snapshot {
	return domain.Snapshot{InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, Price: price}
}

func TestEngine_OnSnapshot_MarksOpenPositionWithoutExitingInsideThresholds(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	opened := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	if _, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Now: opened,
	}); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	// +0.25%: below take_profit (1.2%) and above stop_loss (-0.6%).
	result, err := te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 2005, opened.Add(time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot: %v", err)
	}
	if result.Exited || result.Position == nil {
		t.Fatalf("OnSnapshot() = %+v, want the position still open", result)
	}
	if result.Position.CurrentPrice != 2005 || result.Position.UnrealizedPnL != 500 {
		t.Errorf("marked position = CurrentPrice %v UnrealizedPnL %v, want 2005 / 500", result.Position.CurrentPrice, result.Position.UnrealizedPnL)
	}
}

func TestEngine_OnSnapshot_ClosesPositionWhenStopLossTriggers(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	opened := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Now: opened,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}

	// -1%: past stop_loss_pct=0.6.
	result, err := te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 1980, opened.Add(time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot: %v", err)
	}
	if !result.Exited {
		t.Fatalf("OnSnapshot().Exited = false, want the stop loss to close the position")
	}

	closed, err := te.positions.Get(ctx, entry.Position.ID)
	if err != nil {
		t.Fatalf("Get position: %v", err)
	}
	if closed.IsOpen() || closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonStopLoss {
		t.Errorf("closed position = %+v, want closed with exit reason %q", closed, domain.ExitReasonStopLoss)
	}
	if closed.RealizedPnL == nil || *closed.RealizedPnL != -2000 {
		t.Errorf("RealizedPnL = %v, want -2000", closed.RealizedPnL)
	}
}

func TestEngine_OnSnapshot_FillsPendingLimitEntryOnceCrossed(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	limit := 1990.0
	if _, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeLimit, LimitPrice: &limit, Price: 2000, Now: now,
	}); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	result, err := te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 1995, now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot above limit: %v", err)
	}
	if len(result.Filled) != 0 || result.Position != nil {
		t.Fatalf("OnSnapshot above the BUY limit = %+v, want nothing filled", result)
	}

	result, err = te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 1989, now.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot at crossing price: %v", err)
	}
	if len(result.Filled) != 1 || result.Position == nil || result.Position.EntryPrice != 1989 {
		t.Fatalf("OnSnapshot at crossing price = %+v, want the limit order filled at 1989 and an open position", result)
	}
}

func TestEngine_OnSnapshot_NoPositionIsANoOp(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())

	result, err := te.engine.OnSnapshot(context.Background(), snapshotAt(te.instrument.ID, 2000, time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("OnSnapshot: %v", err)
	}
	if result.Position != nil || result.Exited || len(result.Filled) != 0 {
		t.Errorf("OnSnapshot() = %+v, want an empty result with no position", result)
	}
}

// A missing price (0) must neither stop the position out at -100% nor mark
// it nor fill a pending BUY limit at 0 (issue #173).
func TestEngine_OnSnapshot_RejectsNonPositivePrice(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}

	for _, price := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, price, now.Add(time.Minute))); !errors.Is(err, execution.ErrInvalidPrice) {
			t.Fatalf("OnSnapshot(price=%v) err = %v, want ErrInvalidPrice", price, err)
		}
	}
	if _, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonStopLoss, 0, now); !errors.Is(err, execution.ErrInvalidPrice) {
		t.Fatalf("Close(price=0) err = %v, want ErrInvalidPrice", err)
	}

	got, err := te.positions.Get(ctx, entry.Position.ID)
	if err != nil {
		t.Fatalf("Get position: %v", err)
	}
	if !got.IsOpen() || got.CurrentPrice != 2000 {
		t.Errorf("position = %+v, want still open and unmarked at 2000", got)
	}
}

func TestEngine_TryFillPending_RejectsNonPositivePrice(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	limit := 1990.0
	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeLimit, LimitPrice: &limit, Price: 2000, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	_, ok, err := te.engine.TryFillPending(ctx, entry.Order.ID, domain.JevDirectionLong, 0, now)
	if ok || !errors.Is(err, execution.ErrInvalidPrice) {
		t.Fatalf("TryFillPending(price=0) = ok %v, err %v; want unfilled ErrInvalidPrice", ok, err)
	}
}

// Enter must reject what #173 rejects elsewhere, plus quantity <= 0 (#342).
func TestEngine_Enter_RejectsInvalidPriceOrQuantity(t *testing.T) {
	nan, zero := math.NaN(), 0.0
	cases := map[string]struct {
		req  execution.EntryRequest
		want error
	}{
		"price 0":           {execution.EntryRequest{Quantity: 100, Price: 0}, execution.ErrInvalidPrice},
		"price NaN":         {execution.EntryRequest{Quantity: 100, Price: nan}, execution.ErrInvalidPrice},
		"price +Inf":        {execution.EntryRequest{Quantity: 100, Price: math.Inf(1)}, execution.ErrInvalidPrice},
		"limit price 0":     {execution.EntryRequest{Quantity: 100, Price: 2000, OrderType: domain.OrderTypeLimit, LimitPrice: &zero}, execution.ErrInvalidPrice},
		"limit price NaN":   {execution.EntryRequest{Quantity: 100, Price: 2000, OrderType: domain.OrderTypeLimit, LimitPrice: &nan}, execution.ErrInvalidPrice},
		"quantity 0":        {execution.EntryRequest{Quantity: 0, Price: 2000}, execution.ErrInvalidQuantity},
		"quantity negative": {execution.EntryRequest{Quantity: -100, Price: 2000}, execution.ErrInvalidQuantity},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			te := newTestEngine(t, execution.DefaultConfig())
			tc.req.Signal, tc.req.Now = longSignal(te.instrument.ID), time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
			ctx := context.Background()
			if _, err := te.engine.Enter(ctx, tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("Enter() err = %v, want %v", err, tc.want)
			}
			if _, err := te.positions.GetOpenByInstrument(ctx, te.instrument.ID); !errors.Is(err, domain.ErrPositionNotFound) {
				t.Errorf("GetOpenByInstrument err = %v, want ErrPositionNotFound", err)
			}
		})
	}
}

// Enter must not queue a second entry behind a PENDING one (issue #343).
func TestEngine_Enter_RejectsWhilePendingOrderExists(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	limit := 1990.0
	req := execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeLimit, LimitPrice: &limit, Price: 2000, Now: now,
	}
	if _, err := te.engine.Enter(ctx, req); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if _, err := te.engine.Enter(ctx, req); !errors.Is(err, execution.ErrPendingOrderExists) {
		t.Fatalf("Enter (second, pending) err = %v, want ErrPendingOrderExists", err)
	}
}

// Two PENDING limit orders (e.g. legacy rows) crossing together: the second
// fill loses to positions_open_instrument_uq, but OnSnapshot must still mark
// and evaluate exits for the new position instead of failing every snapshot
// (issue #343).
func TestEngine_OnSnapshot_DuplicatePendingDoesNotBlockExitEvaluation(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	limit := 1990.0
	for i := 0; i < 2; i++ {
		if _, err := te.orders.Insert(ctx, domain.PaperOrder{
			InstrumentID: te.instrument.ID, Symbol: "7203", Side: domain.OrderSideBuy,
			OrderType: domain.OrderTypeLimit, Quantity: 100, LimitPrice: &limit,
			Status: domain.OrderStatusPending, SubmittedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("Insert pending order: %v", err)
		}
	}

	result, err := te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 1989, now.Add(time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot: %v", err)
	}
	if len(result.Filled) != 1 || result.Position == nil {
		t.Fatalf("OnSnapshot() = %+v, want exactly one fill and an open position", result)
	}

	// -1% from the 1989 entry: the stop loss must fire despite the stale order.
	result, err = te.engine.OnSnapshot(ctx, snapshotAt(te.instrument.ID, 1960, now.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot (exit): %v", err)
	}
	if !result.Exited {
		t.Fatalf("OnSnapshot().Exited = false, want the stop loss evaluated and triggered")
	}

	orders, err := te.orders.ListByInstrument(ctx, te.instrument.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	for _, o := range orders {
		if o.Status == domain.OrderStatusPending {
			t.Errorf("order %d still PENDING, want the unfillable duplicate REJECTED", o.ID)
		}
	}
}
