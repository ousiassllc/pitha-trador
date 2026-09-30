package trading_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

func openTestOrderRepo(t *testing.T) (*trading.OrderRepository, int64) {
	t.Helper()
	db := newTestDB(t)
	instID := insertInstrument(t, db, "7203", "トヨタ自動車")
	return trading.NewOrderRepository(db), instID
}

func TestOrderRepository_InsertAndGet_MarketOrder(t *testing.T) {
	repo, instrumentID := openTestOrderRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	created, err := repo.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Side:         domain.OrderSideBuy,
		OrderType:    domain.OrderTypeMarket,
		Quantity:     100,
		Status:       domain.OrderStatusPending,
		SubmittedAt:  now,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}
	if created.LimitPrice != nil {
		t.Fatalf("Insert() = %+v, want nil LimitPrice for a market order", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Side != domain.OrderSideBuy || got.OrderType != domain.OrderTypeMarket || got.Quantity != 100 {
		t.Fatalf("Get(%d) = %+v, want Side/OrderType/Quantity to match Insert input", created.ID, got)
	}
	if got.Status != domain.OrderStatusPending {
		t.Fatalf("Get(%d).Status = %q, want %q", created.ID, got.Status, domain.OrderStatusPending)
	}
	if !got.SubmittedAt.Equal(now) {
		t.Fatalf("Get(%d).SubmittedAt = %v, want %v", created.ID, got.SubmittedAt, now)
	}
}

func TestOrderRepository_InsertAndGet_LimitOrder(t *testing.T) {
	repo, instrumentID := openTestOrderRepo(t)
	ctx := context.Background()
	limitPrice := 2100.0

	created, err := repo.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Side:         domain.OrderSideSell,
		OrderType:    domain.OrderTypeLimit,
		Quantity:     50,
		LimitPrice:   &limitPrice,
		Status:       domain.OrderStatusPending,
		SubmittedAt:  time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.LimitPrice == nil || *got.LimitPrice != limitPrice {
		t.Fatalf("Get(%d).LimitPrice = %v, want %v", created.ID, got.LimitPrice, limitPrice)
	}
}

func TestOrderRepository_Fill_SetsFilledFieldsAndStatus(t *testing.T) {
	repo, instrumentID := openTestOrderRepo(t)
	ctx := context.Background()
	submittedAt := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	filledAt := submittedAt.Add(2 * time.Second)

	created, err := repo.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Side:         domain.OrderSideBuy,
		OrderType:    domain.OrderTypeMarket,
		Quantity:     100,
		Status:       domain.OrderStatusPending,
		SubmittedAt:  submittedAt,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	slippage := 1.5
	filled, err := repo.Fill(ctx, created.ID, 2105.0, &slippage, filledAt)
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if filled.Status != domain.OrderStatusFilled {
		t.Fatalf("Fill().Status = %q, want %q", filled.Status, domain.OrderStatusFilled)
	}
	if filled.FilledPrice == nil || *filled.FilledPrice != 2105.0 {
		t.Fatalf("Fill().FilledPrice = %v, want 2105.0", filled.FilledPrice)
	}
	if filled.FilledAt == nil || !filled.FilledAt.Equal(filledAt) {
		t.Fatalf("Fill().FilledAt = %v, want %v", filled.FilledAt, filledAt)
	}
	if filled.SlippageBps == nil || *filled.SlippageBps != slippage {
		t.Fatalf("Fill().SlippageBps = %v, want %v", filled.SlippageBps, slippage)
	}
}

func TestOrderRepository_Fill_NotFound(t *testing.T) {
	repo, _ := openTestOrderRepo(t)

	_, err := repo.Fill(context.Background(), 999999, 100, nil, time.Now())
	if !errors.Is(err, trading.ErrOrderNotFound) {
		t.Fatalf("Fill(unknown) error = %v, want ErrOrderNotFound", err)
	}
}

func TestOrderRepository_UpdateStatus(t *testing.T) {
	repo, instrumentID := openTestOrderRepo(t)
	ctx := context.Background()

	created, err := repo.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID,
		Symbol:       "7203",
		Side:         domain.OrderSideBuy,
		OrderType:    domain.OrderTypeLimit,
		Quantity:     100,
		LimitPrice:   ptr(2000.0),
		Status:       domain.OrderStatusPending,
		SubmittedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	updated, err := repo.UpdateStatus(ctx, created.ID, domain.OrderStatusCancelled)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Status != domain.OrderStatusCancelled {
		t.Fatalf("UpdateStatus().Status = %q, want %q", updated.Status, domain.OrderStatusCancelled)
	}
}

func TestOrderRepository_Get_NotFound(t *testing.T) {
	repo, _ := openTestOrderRepo(t)

	_, err := repo.Get(context.Background(), 999999)
	if !errors.Is(err, trading.ErrOrderNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrOrderNotFound", err)
	}
}

func TestOrderRepository_ListByInstrument_MostRecentFirstAndRespectsLimit(t *testing.T) {
	repo, instrumentID := openTestOrderRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)

	for i, ts := range []time.Time{base, base.Add(1 * time.Minute), base.Add(2 * time.Minute)} {
		if _, err := repo.Insert(ctx, domain.PaperOrder{
			InstrumentID: instrumentID,
			Symbol:       "7203",
			Side:         domain.OrderSideBuy,
			OrderType:    domain.OrderTypeMarket,
			Quantity:     int64(100 + i),
			Status:       domain.OrderStatusFilled,
			SubmittedAt:  ts,
		}); err != nil {
			t.Fatalf("Insert order %d: %v", i, err)
		}
	}

	got, err := repo.ListByInstrument(ctx, instrumentID, 2)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByInstrument() returned %d rows, want 2 (limit)", len(got))
	}
	if !got[0].SubmittedAt.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("ListByInstrument()[0].SubmittedAt = %v, want the most recent row first", got[0].SubmittedAt)
	}
}

func TestOrderRepository_List_FiltersByStatus(t *testing.T) {
	repo, instrumentID := openTestOrderRepo(t)
	ctx := context.Background()

	if _, err := repo.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Insert filled order: %v", err)
	}
	if _, err := repo.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeLimit, Quantity: 50, LimitPrice: ptr(2000.0),
		Status: domain.OrderStatusPending, SubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Insert pending order: %v", err)
	}

	pending, err := repo.List(ctx, domain.OrderStatusPending, 10)
	if err != nil {
		t.Fatalf("List(pending): %v", err)
	}
	if len(pending) != 1 || pending[0].Status != domain.OrderStatusPending {
		t.Fatalf("List(pending) = %+v, want exactly 1 pending order", pending)
	}

	all, err := repo.List(ctx, "", 10)
	if err != nil {
		t.Fatalf("List(\"\"): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List(\"\") returned %d rows, want 2", len(all))
	}
}

func fillEntryPosition(instrumentID int64, now time.Time) domain.Position {
	return domain.Position{InstrumentID: instrumentID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now}
}

func pendingOrder(t *testing.T, orders *trading.OrderRepository, instrumentID int64, now time.Time) domain.PaperOrder {
	t.Helper()
	o, err := orders.Insert(context.Background(), domain.PaperOrder{InstrumentID: instrumentID, Symbol: "7203",
		Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now})
	if err != nil {
		t.Fatalf("Insert order: %v", err)
	}
	return o
}

// Regression (#158): a failed position open must not leave the order FILLED.
func TestOrderRepository_FillEntry_AtomicOrderFillAndPositionOpen(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx, now := context.Background(), time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)
	first := pendingOrder(t, orders, instrumentID, now)
	filled, position, err := orders.FillEntry(ctx, first.ID, 2100, nil, now, fillEntryPosition(instrumentID, now))
	if err != nil || filled.Status != domain.OrderStatusFilled || position.EntryOrderID != first.ID {
		t.Fatalf("FillEntry = (%+v, %+v, %v), want FILLED order + linked position", filled, position, err)
	}

	second := pendingOrder(t, orders, instrumentID, now)
	if _, _, err := orders.FillEntry(ctx, second.ID, 2110, nil, now, fillEntryPosition(instrumentID, now)); err == nil {
		t.Fatal("FillEntry with an open position = nil error, want the unique-constraint failure")
	}
	if got, err := orders.Get(ctx, second.ID); err != nil || got.Status != domain.OrderStatusPending || got.FilledAt != nil {
		t.Errorf("order after failed FillEntry = (%+v, %v), want untouched PENDING", got, err)
	}
	if open, _ := positions.ListOpen(ctx); len(open) != 1 {
		t.Errorf("ListOpen = %d positions, want only the first", len(open))
	}
}

func TestOrderRepository_ListFilledWithoutPosition_FindsOnlyUnlinkedFillsBeforeCutoff(t *testing.T) {
	_, orders, instrumentID := openTestPositionRepo(t)
	ctx, now := context.Background(), time.Date(2026, 9, 29, 9, 31, 0, 0, time.UTC)
	linked := pendingOrder(t, orders, instrumentID, now)
	if _, _, err := orders.FillEntry(ctx, linked.ID, 2100, nil, now, fillEntryPosition(instrumentID, now)); err != nil {
		t.Fatal(err)
	}
	orphan := insertFilledEntryOrder(t, orders, instrumentID, now) // bare Fill: the pre-#158 failure state
	pendingOrder(t, orders, instrumentID, now)                     // PENDING: not a fill

	got, err := orders.ListFilledWithoutPosition(ctx, now.Add(-time.Hour), now.Add(time.Minute))
	if err != nil || len(got) != 1 || got[0].ID != orphan.ID {
		t.Fatalf("ListFilledWithoutPosition = (%+v, %v), want only order %d", got, err, orphan.ID)
	}
	if got, err := orders.ListFilledWithoutPosition(ctx, now.Add(-time.Hour), now); err != nil || len(got) != 0 {
		t.Fatalf("ListFilledWithoutPosition(before=fill time) = (%+v, %v), want none", got, err)
	}
}
