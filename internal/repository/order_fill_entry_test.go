package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func newFillEntryFixture(t *testing.T) (*repository.OrderRepository, *repository.PositionRepository, int64) {
	t.Helper()
	db := newTestDB(t)
	inst, err := repository.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	return repository.NewOrderRepository(db), repository.NewPositionRepository(db), inst.ID
}

func pendingEntryOrder(t *testing.T, orders *repository.OrderRepository, instrumentID int64, now time.Time) domain.PaperOrder {
	t.Helper()
	o, err := orders.Insert(context.Background(), domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("Insert order: %v", err)
	}
	return o
}

func entryPosition(instrumentID int64, now time.Time) domain.Position {
	return domain.Position{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	}
}

func TestOrderRepository_FillEntry_FillsOrderAndOpensLinkedPosition(t *testing.T) {
	orders, positions, instrumentID := newFillEntryFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)
	order := pendingEntryOrder(t, orders, instrumentID, now)

	filled, position, err := orders.FillEntry(ctx, order.ID, 2100, nil, now, entryPosition(instrumentID, now))
	if err != nil {
		t.Fatalf("FillEntry: %v", err)
	}
	if filled.Status != domain.OrderStatusFilled || position.EntryOrderID != order.ID || !position.IsOpen() {
		t.Fatalf("FillEntry = (%+v, %+v), want FILLED order and open position linked to order %d", filled, position, order.ID)
	}
	if got, err := positions.GetOpenByInstrument(ctx, instrumentID); err != nil || got.ID != position.ID {
		t.Fatalf("GetOpenByInstrument = (%+v, %v), want position %d", got, err, position.ID)
	}
}

// Regression test for issue #158: when the position cannot be opened
// (positions_open_instrument_uq), the order must NOT be left FILLED.
func TestOrderRepository_FillEntry_RollsBackOrderFillWhenPositionOpenFails(t *testing.T) {
	orders, positions, instrumentID := newFillEntryFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)

	first := pendingEntryOrder(t, orders, instrumentID, now)
	if _, _, err := orders.FillEntry(ctx, first.ID, 2100, nil, now, entryPosition(instrumentID, now)); err != nil {
		t.Fatalf("first FillEntry: %v", err)
	}
	second := pendingEntryOrder(t, orders, instrumentID, now.Add(time.Second))

	if _, _, err := orders.FillEntry(ctx, second.ID, 2110, nil, now.Add(time.Second), entryPosition(instrumentID, now)); err == nil {
		t.Fatal("second FillEntry for an instrument with an open position = nil error, want the unique-constraint failure")
	}

	got, err := orders.Get(ctx, second.ID)
	if err != nil {
		t.Fatalf("Get second order: %v", err)
	}
	if got.Status != domain.OrderStatusPending || got.FilledAt != nil || got.FilledPrice != nil {
		t.Errorf("second order after failed FillEntry = %+v, want untouched PENDING", got)
	}
	if open, err := positions.ListOpen(ctx); err != nil || len(open) != 1 {
		t.Errorf("ListOpen = (%d positions, %v), want only the first position", len(open), err)
	}
}

func TestOrderRepository_ListFilledWithoutPosition_FindsOnlyUnlinkedFills(t *testing.T) {
	orders, _, instrumentID := newFillEntryFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)

	// Linked fill (entry of a position): not an orphan.
	linked := pendingEntryOrder(t, orders, instrumentID, now)
	if _, _, err := orders.FillEntry(ctx, linked.ID, 2100, nil, now, entryPosition(instrumentID, now)); err != nil {
		t.Fatalf("FillEntry: %v", err)
	}
	// Bare Fill (the pre-#158 partial-failure state): an orphan.
	orphan := pendingEntryOrder(t, orders, instrumentID, now)
	if _, err := orders.Fill(ctx, orphan.ID, 2100, nil, now); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	// PENDING order: not filled, not an orphan.
	pendingEntryOrder(t, orders, instrumentID, now)

	got, err := orders.ListFilledWithoutPosition(ctx, now.Add(-time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListFilledWithoutPosition: %v", err)
	}
	if len(got) != 1 || got[0].ID != orphan.ID {
		t.Fatalf("ListFilledWithoutPosition = %+v, want only order %d", got, orphan.ID)
	}

	// filledBefore excludes fills newer than the grace cut-off.
	got, err = orders.ListFilledWithoutPosition(ctx, now.Add(-time.Hour), now)
	if err != nil || len(got) != 0 {
		t.Fatalf("ListFilledWithoutPosition(before=fill time) = (%+v, %v), want none", got, err)
	}
}
