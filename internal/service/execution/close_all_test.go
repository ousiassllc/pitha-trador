package execution_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// TestEngine_CloseAll_ForceClosesEveryOpenPositionAtItsCurrentPrice
// exercises FR-RISK-3's "保有ポジション強制クローズ指示" receiving end
// (internal/service/risk.PositionCloser): every open position - across
// every instrument, not just one - closes at its own last known
// CurrentPrice (the mark-to-market value, not EntryPrice), tagged
// domain.ExitReasonForceClose.
func TestEngine_CloseAll_ForceClosesEveryOpenPositionAtItsCurrentPrice(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	// positions_open_instrument_uq allows only one open position per
	// instrument, so exercising "every" open position needs a second
	// instrument.
	inst2, err := te.instruments.Create(ctx, domain.Instrument{
		Symbol: "9984", Name: "ソフトバンクグループ", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create second instrument fixture: %v", err)
	}

	long, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: 2000.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter long: %v", err)
	}
	short, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: shortSignal(inst2.ID), Quantity: 50,
		OrderType: domain.OrderTypeMarket, Price: 8000.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter short: %v", err)
	}

	// Mark both to a price different from EntryPrice so the assertions
	// below actually exercise CloseAll's use of CurrentPrice rather than
	// passing coincidentally against EntryPrice.
	if _, err := te.positions.Mark(ctx, long.Position.ID, 2050.0, 5000.0, now); err != nil {
		t.Fatalf("Mark long: %v", err)
	}
	if _, err := te.positions.Mark(ctx, short.Position.ID, 7900.0, 5000.0, now); err != nil {
		t.Fatalf("Mark short: %v", err)
	}

	if err := te.engine.CloseAll(ctx, domain.KillReasonDailyLossLimit); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}

	closedLong, err := te.positions.Get(ctx, long.Position.ID)
	if err != nil {
		t.Fatalf("Get long: %v", err)
	}
	if closedLong.IsOpen() {
		t.Fatalf("long position still open after CloseAll: %+v", closedLong)
	}
	if closedLong.CurrentPrice != 2050.0 {
		t.Fatalf("long ClosedPrice = %v, want 2050.0 (its marked CurrentPrice)", closedLong.CurrentPrice)
	}
	if closedLong.ExitReason == nil || *closedLong.ExitReason != domain.ExitReasonForceClose {
		t.Fatalf("long ExitReason = %v, want %q", closedLong.ExitReason, domain.ExitReasonForceClose)
	}
	if closedLong.RealizedPnL == nil || *closedLong.RealizedPnL != 5000.0 {
		t.Fatalf("long RealizedPnL = %v, want 5000.0 (LONG, price rose 2000->2050 over qty 100)", closedLong.RealizedPnL)
	}

	closedShort, err := te.positions.Get(ctx, short.Position.ID)
	if err != nil {
		t.Fatalf("Get short: %v", err)
	}
	if closedShort.IsOpen() {
		t.Fatalf("short position still open after CloseAll: %+v", closedShort)
	}
	if closedShort.CurrentPrice != 7900.0 {
		t.Fatalf("short ClosedPrice = %v, want 7900.0 (its marked CurrentPrice)", closedShort.CurrentPrice)
	}
	if closedShort.ExitReason == nil || *closedShort.ExitReason != domain.ExitReasonForceClose {
		t.Fatalf("short ExitReason = %v, want %q", closedShort.ExitReason, domain.ExitReasonForceClose)
	}
	if closedShort.RealizedPnL == nil || *closedShort.RealizedPnL != 5000.0 {
		t.Fatalf("short RealizedPnL = %v, want 5000.0 (SHORT, price fell 8000->7900 over qty 50)", closedShort.RealizedPnL)
	}

	open, err := te.positions.ListOpen(ctx)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("ListOpen after CloseAll = %+v, want empty", open)
	}
}

// TestEngine_CloseAll_NoOpenPositionsIsANoOp confirms CloseAll does not
// error merely because Kill Switch fired while no position was open
// (the common case).
func TestEngine_CloseAll_NoOpenPositionsIsANoOp(t *testing.T) {
	te := newTestEngine(t, execution.Config{})

	if err := te.engine.CloseAll(context.Background(), domain.KillReasonBrokerAPIError); err != nil {
		t.Fatalf("CloseAll with no open positions: %v", err)
	}
}
