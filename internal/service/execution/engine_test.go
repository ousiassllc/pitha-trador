package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// testEngine bundles a real (in-memory-SQLite-backed) Engine plus its
// underlying instrument fixture, mirroring the pattern
// internal/service/risk's tests use for a real repository.KillSwitchRepository
// (rather than a hand-rolled fake).
type testEngine struct {
	engine      *execution.Engine
	orders      *repository.OrderRepository
	positions   *repository.PositionRepository
	instruments *repository.InstrumentRepository
	snapshots   *repository.SnapshotRepository
	decisions   *repository.DecisionRepository
	signals     *repository.SignalRepository
	instrument  domain.Instrument
}

func newTestEngine(t *testing.T, cfg execution.Config) testEngine {
	t.Helper()
	db := newTestDB(t)

	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	orders := repository.NewOrderRepository(db)
	positions := repository.NewPositionRepository(db)
	snapshots := repository.NewSnapshotRepository(db)
	decisions := repository.NewDecisionRepository(db)
	signals := repository.NewSignalRepository(db)

	engine := execution.NewEngine(execution.Deps{
		Orders:      orders,
		Positions:   positions,
		Snapshots:   snapshots,
		Decisions:   decisions,
		Signals:     signals,
		Instruments: instruments,
	}, cfg)

	return testEngine{
		engine: engine, orders: orders, positions: positions,
		instruments: instruments, snapshots: snapshots, decisions: decisions, signals: signals,
		instrument: inst,
	}
}

func longSignal(instrumentID int64) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: instrumentID, Symbol: "7203", Direction: domain.JevDirectionLong,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func shortSignal(instrumentID int64) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: instrumentID, Symbol: "7203", Direction: domain.JevDirectionShort,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func TestEngine_Enter_MarketOrderFillsImmediatelyAndOpensPosition(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	result, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if result.Order.Status != domain.OrderStatusFilled {
		t.Fatalf("Enter().Order.Status = %q, want %q", result.Order.Status, domain.OrderStatusFilled)
	}
	if result.Position == nil {
		t.Fatalf("Enter().Position = nil, want an opened LONG position for a market order")
	}
	if result.Position.Side != domain.PositionSideLong || result.Position.EntryPrice != 2100.0 || result.Position.Quantity != 100 {
		t.Fatalf("Enter().Position = %+v, want Side=LONG EntryPrice=2100.0 Quantity=100", result.Position)
	}
}

func TestEngine_Enter_ShortSignalOpensShortPositionViaSellOrder(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()

	result, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: shortSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if result.Order.Side != domain.OrderSideSell {
		t.Fatalf("Enter().Order.Side = %q, want %q for a SHORT entry", result.Order.Side, domain.OrderSideSell)
	}
	if result.Position == nil || result.Position.Side != domain.PositionSideShort {
		t.Fatalf("Enter().Position = %+v, want Side=SHORT", result.Position)
	}
}

func TestEngine_Enter_LimitOrderStaysPendingUntilPriceCrosses(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	limitPrice := 2090.0

	result, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeLimit, LimitPrice: &limitPrice,
		Price: 2100.0, // above the buy limit: does not cross yet
		Now:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if result.Order.Status != domain.OrderStatusPending {
		t.Fatalf("Enter().Order.Status = %q, want %q (limit not crossed)", result.Order.Status, domain.OrderStatusPending)
	}
	if result.Position != nil {
		t.Fatalf("Enter().Position = %+v, want nil while the limit order is still pending", result.Position)
	}

	filled, ok, err := te.engine.TryFillPending(ctx, result.Order.ID, domain.JevDirectionLong, 2089.0, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryFillPending: %v", err)
	}
	if !ok {
		t.Fatalf("TryFillPending() ok = false, want true once price crosses the buy limit")
	}
	if filled.Position == nil || filled.Position.EntryPrice != 2089.0 {
		t.Fatalf("TryFillPending().Position = %+v, want EntryPrice=2089.0", filled.Position)
	}
}

func TestEngine_Enter_LimitOrderFillsImmediatelyWhenAlreadyCrossed(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	limitPrice := 2100.0

	result, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeLimit, LimitPrice: &limitPrice,
		Price: 2095.0, // already at-or-below the buy limit
		Now:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if result.Order.Status != domain.OrderStatusFilled {
		t.Fatalf("Enter().Order.Status = %q, want %q (limit already crossed)", result.Order.Status, domain.OrderStatusFilled)
	}
	if result.Position == nil {
		t.Fatalf("Enter().Position = nil, want an opened position")
	}
}

func TestEngine_Enter_RejectsNoneOrInvalidDirection(t *testing.T) {
	te := newTestEngine(t, execution.Config{})

	_, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal:   domain.TradeSignal{InstrumentID: te.instrument.ID, Symbol: "7203", Direction: domain.JevDirectionNone, RiskPassed: false},
		Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: time.Now().UTC(),
	})
	if !errors.Is(err, execution.ErrDirectionInvalid) {
		t.Fatalf("Enter(NONE direction) error = %v, want ErrDirectionInvalid", err)
	}
}

func TestEngine_Enter_RejectsRiskNotPassed(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	signal := longSignal(te.instrument.ID)
	signal.RiskPassed = false

	_, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: signal, Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: time.Now().UTC(),
	})
	if !errors.Is(err, execution.ErrRiskNotPassed) {
		t.Fatalf("Enter(RiskPassed=false) error = %v, want ErrRiskNotPassed", err)
	}
}

func TestEngine_Enter_RejectsWhenPositionAlreadyOpen(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: now,
	}); err != nil {
		t.Fatalf("Enter (first): %v", err)
	}

	_, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2110.0, Now: now,
	})
	if !errors.Is(err, execution.ErrPositionAlreadyOpen) {
		t.Fatalf("Enter (second, already open) error = %v, want ErrPositionAlreadyOpen", err)
	}
}

func TestEngine_Enter_RejectsLimitOrderWithoutLimitPrice(t *testing.T) {
	te := newTestEngine(t, execution.Config{})

	_, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeLimit, Price: 2100.0, Now: time.Now().UTC(),
	})
	if !errors.Is(err, execution.ErrLimitPriceRequired) {
		t.Fatalf("Enter(LIMIT, no LimitPrice) error = %v, want ErrLimitPriceRequired", err)
	}
}

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
