package pendingfill_test

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

func insertPendingMarket(t *testing.T, te testEngine, submittedAt time.Time) domain.PaperOrder {
	t.Helper()
	o, err := te.orders.Insert(context.Background(), domain.PaperOrder{
		InstrumentID: te.instrument.ID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100,
		Status: domain.OrderStatusPending, SubmittedAt: submittedAt,
	})
	if err != nil {
		t.Fatalf("Insert pending market order: %v", err)
	}
	return o
}

func orderStatus(t *testing.T, te testEngine, id int64) string {
	t.Helper()
	o, err := te.orders.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get order %d: %v", id, err)
	}
	return o.Status
}

// An orphaned PENDING market order (left by a crash between Insert and
// FillEntry) must not block the instrument's entries for ever (#627): Enter
// rejects it and proceeds.
func TestEngine_Enter_RejectsStalePendingMarketOrderAndEnters(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	stale := insertPendingMarket(t, te, now.Add(-10*time.Minute))

	res, err := te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: 2000, Now: now,
	})
	if err != nil {
		t.Fatalf("Enter with a stale PENDING market order: %v", err)
	}
	if res.Position == nil {
		t.Fatalf("Enter result has no position: %+v", res)
	}
	if got := orderStatus(t, te, stale.ID); got != domain.OrderStatusRejected {
		t.Fatalf("stale market order status = %q, want REJECTED", got)
	}
}

// A just-submitted PENDING market order may be a concurrent Enter's
// in-flight order, so it keeps blocking (and is left untouched).
func TestEngine_Enter_FreshPendingMarketOrderStillBlocks(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	fresh := insertPendingMarket(t, te, now.Add(-time.Second))

	_, err := te.engine.Enter(context.Background(), execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: 2000, Now: now,
	})
	if !errors.Is(err, execution.ErrPendingOrderExists) {
		t.Fatalf("Enter err = %v, want ErrPendingOrderExists", err)
	}
	if got := orderStatus(t, te, fresh.ID); got != domain.OrderStatusPending {
		t.Fatalf("fresh market order status = %q, want PENDING", got)
	}
}

func TestEngine_OnSnapshot_RejectsStalePendingMarketOrder(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	stale := insertPendingMarket(t, te, now.Add(-10*time.Minute))

	if _, err := te.engine.OnSnapshot(context.Background(), snapshotAt(te.instrument.ID, 2000, now)); err != nil {
		t.Fatalf("OnSnapshot: %v", err)
	}
	if got := orderStatus(t, te, stale.ID); got != domain.OrderStatusRejected {
		t.Fatalf("stale market order status = %q, want REJECTED", got)
	}
}

// cancelOnceOrderExistsCtx behaves like a live context until a paper_orders
// row exists (checked through a second connection, so no pool deadlock),
// then reports itself cancelled - the shutdown landing between Enter's order
// INSERT and its FillEntry.
type cancelOnceOrderExistsCtx struct {
	context.Context
	probe     *sql.DB
	cancelled atomic.Bool
	done      chan struct{}
}

func (c *cancelOnceOrderExistsCtx) check() bool {
	if c.cancelled.Load() {
		return true
	}
	var n int
	if err := c.probe.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM paper_orders`).Scan(&n); err == nil && n > 0 {
		if c.cancelled.CompareAndSwap(false, true) {
			close(c.done)
		}
		return true
	}
	return false
}

func (c *cancelOnceOrderExistsCtx) Err() error {
	if c.check() {
		return context.Canceled
	}
	return nil
}

func (c *cancelOnceOrderExistsCtx) Done() <-chan struct{} {
	if c.check() {
		return c.done
	}
	return nil
}

// When fillEntry fails because ctx was cancelled, the clean-up REJECT must
// not fail for the same reason and leave the order PENDING (#627).
func TestEngine_Enter_FillFailureWithCancelledCtxStillRejectsOrder(t *testing.T) {
	te := newTestEngine(t, execution.DefaultConfig())
	probe, err := sqlitedb.Open(te.dbPath)
	if err != nil {
		t.Fatalf("open probe connection: %v", err)
	}
	t.Cleanup(func() { _ = probe.Close() })
	ctx := &cancelOnceOrderExistsCtx{Context: context.Background(), probe: probe, done: make(chan struct{})}
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	_, err = te.engine.Enter(ctx, execution.EntryRequest{
		Signal: longSignal(te.instrument.ID), Quantity: 100,
		OrderType: domain.OrderTypeMarket, Price: 2000, Now: now,
	})
	if err == nil {
		t.Fatal("Enter error = nil, want the fill failure")
	}
	orders, err := te.orders.ListByInstrument(context.Background(), te.instrument.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(orders) != 1 || orders[0].Status != domain.OrderStatusRejected {
		t.Fatalf("orders = %+v, want exactly one REJECTED order (not left PENDING)", orders)
	}
}
