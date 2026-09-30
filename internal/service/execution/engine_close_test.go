package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func TestEngine_Close_LosingTradeStartsSymbolCooldown(t *testing.T) {
	cfg := execution.Config{CooldownAfterLossMinutes: 5}
	te := newTestEngine(t, cfg)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}

	closedAt := now.Add(2 * time.Minute)
	closed, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonStopLoss, 2088.0, closedAt)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.RealizedPnL == nil || *closed.RealizedPnL != -1200.0 {
		t.Fatalf("Close().RealizedPnL = %v, want -1200.0 (100 * (2088-2100))", closed.RealizedPnL)
	}
	if closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonStopLoss {
		t.Fatalf("Close().ExitReason = %v, want %q", closed.ExitReason, domain.ExitReasonStopLoss)
	}

	// Immediately re-entering the same symbol must be rejected: still
	// within the 5-minute post-loss cooldown.
	_, err = te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket,
		Price: 2090.0, Now: closedAt.Add(1 * time.Minute),
	})
	if !errors.Is(err, execution.ErrSymbolInCooldown) {
		t.Fatalf("Enter (within cooldown) error = %v, want ErrSymbolInCooldown", err)
	}

	// Past the cooldown window, entry succeeds again.
	_, err = te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket,
		Price: 2090.0, Now: closedAt.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Enter (after cooldown): %v", err)
	}
}

func TestEngine_Close_WinningTradeDoesNotStartCooldown(t *testing.T) {
	te := newTestEngine(t, execution.Config{CooldownAfterLossMinutes: 5})
	ctx := context.Background()
	now := time.Now().UTC()

	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	closed, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonTakeProfit, 2130.0, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.RealizedPnL == nil || *closed.RealizedPnL <= 0 {
		t.Fatalf("Close().RealizedPnL = %v, want a positive value", closed.RealizedPnL)
	}

	if _, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket,
		Price: 2100.0, Now: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("Enter (after a winning close, no cooldown expected): %v", err)
	}
}

func TestEngine_Close_ShortPositionRealizedPnLSignIsInverted(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Now().UTC()

	entry, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: shortSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}

	closed, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonManual, 2080.0, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.RealizedPnL == nil || *closed.RealizedPnL != 2000.0 {
		t.Fatalf("Close().RealizedPnL = %v, want 2000.0 (SHORT profits when price falls: 100 * (2100-2080))", closed.RealizedPnL)
	}
}
