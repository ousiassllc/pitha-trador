package closeflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
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

// Kill Switch fired outside the 立会時間 (FR-ENTRY / FR-RISK-3,
// components-pipeline.md): CloseAll fills nothing and keeps the position
// open, returns an error wrapping execution.ErrOutsideTradingSession, and
// the same CloseAll at the next session (risk.Engine.RetryForceClose's
// 1-minute retry) force-closes it.
func TestEngine_CloseAll_OutsideTradingSessionKeepsPositionUntilNextSession(t *testing.T) {
	jstAt := func(d, hh, mm int) time.Time {
		return time.Date(2026, time.September, d, hh, mm, 0, 0, marketcalendar.JST)
	}
	for name, at := range map[string]time.Time{
		"lunch break":     jstAt(29, 11, 45),
		"after the close": jstAt(29, 15, 30),
		"night":           jstAt(29, 20, 0),
		"saturday":        jstAt(26, 10, 0),
		"holiday":         jstAt(22, 10, 0),
	} {
		t.Run(name, func(t *testing.T) {
			now := jstAt(29, 10, 0)
			cfg := execution.DefaultConfig()
			cfg.Calendar = marketcalendar.TSE
			cfg.Now = func() time.Time { return now }
			te := newTestEngine(t, cfg)
			ctx := context.Background()

			entry, err := te.engine.Enter(ctx, execution.EntryRequest{
				Signal: longSignal(te.instrument.ID), Quantity: 100,
				OrderType: domain.OrderTypeMarket, Price: 2000.0, Now: now,
			})
			if err != nil {
				t.Fatalf("Enter: %v", err)
			}
			if _, err := te.positions.Mark(ctx, entry.Position.ID, 2050.0, 5000.0, now); err != nil {
				t.Fatalf("Mark: %v", err)
			}
			ordersBefore, err := te.orders.List(ctx, "", 100)
			if err != nil {
				t.Fatalf("List orders: %v", err)
			}

			now = at
			err = te.engine.CloseAll(ctx, domain.KillReasonDailyLossLimit)
			if !errors.Is(err, execution.ErrOutsideTradingSession) {
				t.Fatalf("CloseAll at %s err = %v, want ErrOutsideTradingSession", at.Format("2006-01-02 15:04"), err)
			}
			held, err := te.positions.Get(ctx, entry.Position.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !held.IsOpen() {
				t.Fatalf("position closed outside the session: %+v", held)
			}
			ordersAfter, err := te.orders.List(ctx, "", 100)
			if err != nil {
				t.Fatalf("List orders: %v", err)
			}
			if len(ordersBefore) != 1 || len(ordersAfter) != len(ordersBefore) {
				t.Fatalf("orders after refused CloseAll = %d, want 1 (the entry order only, no exit order)", len(ordersAfter))
			}

			now = jstAt(30, 9, 30) // the next session
			if err := te.engine.CloseAll(ctx, domain.KillReasonDailyLossLimit); err != nil {
				t.Fatalf("CloseAll at the next session: %v", err)
			}
			closed, err := te.positions.Get(ctx, entry.Position.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if closed.IsOpen() {
				t.Fatalf("position still open after CloseAll at the next session: %+v", closed)
			}
			if closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonForceClose {
				t.Fatalf("ExitReason = %v, want %q", closed.ExitReason, domain.ExitReasonForceClose)
			}
		})
	}
}
