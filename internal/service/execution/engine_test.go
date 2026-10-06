package execution_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
)

// testEngine bundles a real (SQLite-backed) Engine plus its underlying
// instrument fixture (as internal/service/risk's tests do for the kill switch).
type testEngine struct {
	db          *sql.DB
	engine      *execution.Engine
	orders      *trading.OrderRepository
	positions   *trading.PositionRepository
	instruments *market.InstrumentRepository
	snapshots   *market.SnapshotRepository
	decisions   *judgement.DecisionRepository
	signals     *trading.SignalRepository
	instrument  domain.Instrument
}

func newTestEngine(t *testing.T, cfg execution.Config) testEngine {
	t.Helper()
	db := newTestDB(t)

	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	orders := trading.NewOrderRepository(db)
	positions := trading.NewPositionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	decisions := judgement.NewDecisionRepository(db)
	signals := trading.NewSignalRepository(db)

	engine := execution.NewEngine(execution.Deps{
		Orders:      orders,
		Positions:   positions,
		Snapshots:   snapshots,
		Decisions:   decisions,
		Signals:     signals,
		Instruments: instruments,
	}, cfg)

	return testEngine{
		db: db, engine: engine, orders: orders, positions: positions, snapshots: snapshots,
		instruments: instruments, decisions: decisions, signals: signals, instrument: inst,
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

	filled, ok, err := te.engine.TryFillPending(ctx, result.Order.ID, domain.JevDirectionLong, 2089.0, fillmodel.Book{}, time.Now().UTC())
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

// Regression test for issue #158: an entry whose position cannot be opened
// must leave no FILLED order behind.
func TestEngine_Enter_PositionOpenFailureLeavesNoFilledOrder(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER positions_block BEFORE INSERT ON positions BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	orders := trading.NewOrderRepository(db)
	engine := execution.NewEngine(execution.Deps{Orders: orders, Positions: trading.NewPositionRepository(db)}, execution.Config{})

	_, err = engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(inst.ID), Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100,
		Now: time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Enter = nil error, want the position insert failure")
	}

	all, err := orders.List(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("List orders: %v", err)
	}
	if len(all) != 1 || all[0].Status != domain.OrderStatusRejected || all[0].FilledAt != nil {
		t.Fatalf("orders after failed Enter = %+v, want exactly one REJECTED, never FILLED", all)
	}
}

func TestEngine_TryFillPending_PositionOpenFailureKeepsOrderPending(t *testing.T) {
	te := newTestEngine(t, execution.Config{})
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)
	limit := 2000.0

	result, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100, OrderType: domain.OrderTypeLimit,
		LimitPrice: &limit, Price: 2100, Now: now,
	})
	if err != nil || result.Order.Status != domain.OrderStatusPending {
		t.Fatalf("Enter limit = (%+v, %v), want PENDING order", result, err)
	}
	// Another position appears for the instrument before the limit crosses.
	other, err := te.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: te.instrument.ID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
		Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert competing order: %v", err)
	}
	if _, err := te.positions.Open(ctx, domain.Position{
		InstrumentID: te.instrument.ID, EntryOrderID: other.ID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	}); err != nil {
		t.Fatalf("open competing position: %v", err)
	}

	if _, ok, err := te.engine.TryFillPending(ctx, result.Order.ID, domain.JevDirectionLong, 1990, fillmodel.Book{}, now); err == nil || ok {
		t.Fatalf("TryFillPending = (ok=%v, err=%v), want error", ok, err)
	}

	got, err := te.orders.Get(ctx, result.Order.ID)
	if err != nil || got.Status != domain.OrderStatusPending {
		t.Fatalf("order after failed TryFillPending = (%+v, %v), want PENDING", got, err)
	}
}
