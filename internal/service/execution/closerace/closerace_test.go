// Package closerace_test holds the issue #174 regression tests: exits of one
// position racing (manual close / CloseAll / Exit monitor) must close it
// exactly once and never leave an orphan FILLED exit order. It lives in its
// own directory to keep internal/service/execution and internal/repository
// within the per-directory line budget.
package closerace_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

var now = time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)

type fixture struct {
	engine    *execution.Engine
	orders    *repository.OrderRepository
	positions *repository.PositionRepository
	position  domain.Position
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	db, err := repository.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	inst, err := repository.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{orders: repository.NewOrderRepository(db), positions: repository.NewPositionRepository(db)}
	f.engine = execution.NewEngine(execution.Deps{
		Orders: f.orders, Positions: f.positions, Snapshots: repository.NewSnapshotRepository(db),
		Decisions: repository.NewDecisionRepository(db), Signals: repository.NewSignalRepository(db),
		Instruments: repository.NewInstrumentRepository(db),
	}, execution.Config{})
	entry, err := f.engine.Enter(ctx, execution.EntryRequest{
		Signal:   domain.TradeSignal{InstrumentID: inst.ID, Symbol: "7203", Direction: domain.JevDirectionLong, RiskPassed: true, PolicyVersion: "v1"},
		Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	f.position = *entry.Position
	return f
}

func (f fixture) filledOrders(t *testing.T) (filled, orphans int) {
	t.Helper()
	ctx := context.Background()
	all, err := f.orders.List(ctx, domain.OrderStatusFilled, 100)
	if err != nil {
		t.Fatal(err)
	}
	o, err := f.orders.ListFilledWithoutPosition(ctx, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return len(all), len(o)
}

func TestEngine_Close_ConcurrentCallsCloseOnceWithoutOrphanFilledOrder(t *testing.T) {
	f := newFixture(t)
	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.engine.Close(context.Background(), f.position.ID, domain.ExitReasonManual, 2110, now.Add(time.Minute))
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case !errors.Is(err, domain.ErrPositionAlreadyClosed):
			t.Errorf("losing Close err = %v, want ErrPositionAlreadyClosed", err)
		}
	}
	if filled, orphans := f.filledOrders(t); succeeded != 1 || filled != 2 || orphans != 0 {
		t.Fatalf("succeeded=%d filled=%d orphans=%d, want 1 winner, 2 FILLED (entry+exit), 0 orphans", succeeded, filled, orphans)
	}
}

// The transaction alone (bypassing Engine's mutex) must roll the losing
// exit order back.
func TestPositionRepository_CloseWithExitOrder_LosingCloseRollsBackOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	exit := domain.PaperOrder{
		InstrumentID: f.position.InstrumentID, Symbol: "7203", Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
	}
	closed, err := f.positions.CloseWithExitOrder(ctx, exit, 2110, f.position.ID, 1000, domain.ExitReasonManual, now)
	if err != nil || closed.ExitOrderID == nil || closed.IsOpen() {
		t.Fatalf("first CloseWithExitOrder = (%+v, %v), want closed position linked to the exit order", closed, err)
	}
	if _, err := f.positions.CloseWithExitOrder(ctx, exit, 2120, f.position.ID, 2000, domain.ExitReasonManual, now); !errors.Is(err, domain.ErrPositionNotFound) {
		t.Fatalf("second CloseWithExitOrder err = %v, want ErrPositionNotFound", err)
	}
	if filled, orphans := f.filledOrders(t); filled != 2 || orphans != 0 {
		t.Fatalf("filled=%d orphans=%d, want 2 and 0 (losing order rolled back)", filled, orphans)
	}
}

func TestOrderRepository_Fill_RejectsAlreadyFilledOrder(t *testing.T) {
	f := newFixture(t)
	if _, err := f.orders.Fill(context.Background(), f.position.EntryOrderID, 9999, nil, now); !errors.Is(err, repository.ErrOrderNotPending) {
		t.Fatalf("Fill on FILLED entry order err = %v, want ErrOrderNotPending", err)
	}
}
