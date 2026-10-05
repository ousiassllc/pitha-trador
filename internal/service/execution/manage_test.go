package execution_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
)

func snapshotAt(instrumentID int64, price float64, at time.Time) domain.Snapshot {
	return domain.Snapshot{InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, Price: price}
}

// costlessConfig is DefaultConfig without fill costs: tests of exit/limit
// logic that pin exact prices, as opposed to fill_test.go's cost model.
func costlessConfig() execution.Config {
	cfg := execution.DefaultConfig()
	cfg.Fill = fillmodel.Model{}
	return cfg
}

func TestEngine_OnSnapshot_MarksOpenPositionWithoutExitingInsideThresholds(t *testing.T) {
	te := newTestEngine(t, costlessConfig())
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
	te := newTestEngine(t, costlessConfig())
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

// enterOpenPosition opens a 100-share LONG at 2000 and returns its
// position ID and entry time.
func enterOpenPosition(t *testing.T, te testEngine) (int64, time.Time) {
	t.Helper()
	opened := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	entry, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, Price: 2000, Now: opened,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	return entry.Position.ID, opened
}

// Regression test for issue #460: a failing Mark must report the real
// position ID, not the zero value Mark returns alongside its error.
func TestEngine_OnSnapshot_MarkFailureReportsPositionID(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	positionID, opened := enterOpenPosition(t, te)
	if _, err := te.db.Exec(`CREATE TRIGGER positions_busy BEFORE UPDATE ON positions BEGIN SELECT RAISE(ABORT, 'database is locked'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err := te.engine.OnSnapshot(context.Background(), snapshotAt(te.instrument.ID, 2005, opened.Add(time.Minute)))
	want := fmt.Sprintf("mark position %d to market", positionID)
	if err == nil || errors.Is(err, domain.ErrPositionNotFound) || !strings.Contains(err.Error(), want) {
		t.Fatalf("OnSnapshot error = %v, want a non-NotFound error containing %q", err, want)
	}
}

// Regression test for issue #526: a manual close / CloseAll that wins the
// race between GetOpenByInstrument and Mark is a normal outcome, not an
// error that fails the whole market-data job.
func TestEngine_OnSnapshot_PositionClosedBeforeMarkIsNotAnError(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	_, opened := enterOpenPosition(t, te)
	// RAISE(IGNORE) makes Mark's UPDATE touch no row, as if the position was closed concurrently.
	if _, err := te.db.Exec(`CREATE TRIGGER positions_skip BEFORE UPDATE ON positions BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	result, err := te.engine.OnSnapshot(context.Background(), snapshotAt(te.instrument.ID, 2005, opened.Add(time.Minute)))
	if err != nil {
		t.Fatalf("OnSnapshot: %v, want no error for a concurrently closed position", err)
	}
	if result.Position != nil || result.Exited {
		t.Errorf("OnSnapshot() = %+v, want Position=nil, Exited=false", result)
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
