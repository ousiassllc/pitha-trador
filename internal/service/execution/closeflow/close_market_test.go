package closeflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func enterLong2100(t *testing.T, te testEngine, now time.Time) domain.Position {
	t.Helper()
	entry, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	return *entry.Position
}

// CloseAtMarket picks the fill-model Book itself (#604): the latest
// snapshot's bid/ask is used when it is the bar CurrentPrice was marked at,
// so a LONG exit (a SELL) fills at the bid, not at the bare price.
func TestEngine_CloseAtMarket_FillsAgainstLatestSnapshotBook(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	position := enterLong2100(t, te, now)

	bid, ask := 2098.0, 2102.0
	if _, err := te.snapshots.Insert(ctx, domain.Snapshot{
		InstrumentID: te.instrument.ID, Symbol: "7203", Timestamp: now, Price: position.CurrentPrice,
		Volume: 1000, Turnover: 2100000, Bid: &bid, Ask: &ask,
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	closed, err := te.engine.CloseAtMarket(ctx, position.ID, domain.ExitReasonManual, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("CloseAtMarket: %v", err)
	}
	if closed.RealizedPnL == nil || *closed.RealizedPnL != -200.0 {
		t.Fatalf("RealizedPnL = %v, want -200 (SELL fills at the 2098 bid: 100 * (2098-2100))", closed.RealizedPnL)
	}
	if closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonManual {
		t.Fatalf("ExitReason = %v, want %q", closed.ExitReason, domain.ExitReasonManual)
	}
}

// A failing snapshot read must not turn into a zero-Book (spread-free)
// fill: CloseAtMarket fails and the position stays open.
func TestEngine_CloseAtMarket_SnapshotReadFailureDoesNotFillWithZeroBook(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	position := enterLong2100(t, te, now)

	if _, err := te.db.ExecContext(ctx, `DROP TABLE market_snapshots`); err != nil {
		t.Fatalf("break snapshot reads: %v", err)
	}

	if _, err := te.engine.CloseAtMarket(ctx, position.ID, domain.ExitReasonManual, now.Add(time.Minute)); err == nil {
		t.Fatal("CloseAtMarket error = nil, want the snapshot read failure")
	}
	got, err := te.positions.Get(ctx, position.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsOpen() {
		t.Fatalf("position was closed despite the snapshot read failure: %+v", got)
	}
}

func TestEngine_CloseAtMarket_AlreadyClosedPosition(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	position := enterLong2100(t, te, now)
	if _, err := te.engine.CloseAtMarket(ctx, position.ID, domain.ExitReasonManual, now); err != nil {
		t.Fatalf("first CloseAtMarket: %v", err)
	}
	_, err := te.engine.CloseAtMarket(ctx, position.ID, domain.ExitReasonManual, now)
	if !errors.Is(err, domain.ErrPositionAlreadyClosed) {
		t.Fatalf("second CloseAtMarket error = %v, want ErrPositionAlreadyClosed", err)
	}
}
