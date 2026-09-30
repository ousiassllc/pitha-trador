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
