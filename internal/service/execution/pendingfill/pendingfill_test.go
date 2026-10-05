// Package pendingfill_test holds the PENDING limit-entry tests of
// execution.Engine (OnSnapshot fills, TryFillPending, duplicate PENDING
// orders; issues #173 / #343). They only use execution's exported API and
// live in their own directory to keep internal/service/execution under the
// linterly line budget (#348). The helpers below are this package's own
// (sibling test packages do not import each other).
package pendingfill_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
)

// testEngine bundles a real (SQLite-backed) Engine plus the repositories and
// instrument fixture the pending-fill tests need.
type testEngine struct {
	engine     *execution.Engine
	orders     *trading.OrderRepository
	positions  *trading.PositionRepository
	instrument domain.Instrument
}

func newTestEngine(t *testing.T, cfg execution.Config) testEngine {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})

	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	orders := trading.NewOrderRepository(db)
	positions := trading.NewPositionRepository(db)
	engine := execution.NewEngine(execution.Deps{
		Orders:      orders,
		Positions:   positions,
		Snapshots:   market.NewSnapshotRepository(db),
		Decisions:   judgement.NewDecisionRepository(db),
		Signals:     trading.NewSignalRepository(db),
		Instruments: instruments,
	}, cfg)

	return testEngine{engine: engine, orders: orders, positions: positions, instrument: inst}
}

func longSignal(instrumentID int64) domain.TradeSignal {
	return domain.TradeSignal{
		InstrumentID: instrumentID, Symbol: "7203", Direction: domain.JevDirectionLong,
		RiskPassed: true, PolicyVersion: "v1",
	}
}

func snapshotAt(instrumentID int64, price float64, at time.Time) domain.Snapshot {
	return domain.Snapshot{InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, Price: price}
}

// costlessConfig is DefaultConfig without fill costs, for tests that pin
// exact fill prices (the cost model is covered by ../fill_test.go).
func costlessConfig() execution.Config {
	cfg := execution.DefaultConfig()
	cfg.Fill = fillmodel.Model{}
	return cfg
}

func TestEngine_OnSnapshot_FillsPendingLimitEntryOnceCrossed(t *testing.T) {
	te := newTestEngine(t, costlessConfig())
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

// A missing price (0) must neither stop the position out at -100% nor mark
// it nor fill a pending BUY limit at 0 (issue #173).
func TestEngine_OnSnapshot_RejectsNonPositivePrice(t *testing.T) {
	te := newTestEngine(t, costlessConfig())
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
	if _, err := te.engine.Close(ctx, entry.Position.ID, domain.ExitReasonStopLoss, 0, fillmodel.Book{}, now); !errors.Is(err, execution.ErrInvalidPrice) {
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
	_, ok, err := te.engine.TryFillPending(ctx, entry.Order.ID, domain.JevDirectionLong, 0, fillmodel.Book{}, now)
	if ok || !errors.Is(err, execution.ErrInvalidPrice) {
		t.Fatalf("TryFillPending(price=0) = ok %v, err %v; want unfilled ErrInvalidPrice", ok, err)
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
