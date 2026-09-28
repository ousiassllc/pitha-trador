package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func openTestOrderRepo(t *testing.T) (*repository.OrderRepository, int64) {
	t.Helper()
	db := newTestDB(t)

	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	return repository.NewOrderRepository(db), inst.ID
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
	if !errors.Is(err, repository.ErrOrderNotFound) {
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
	if !errors.Is(err, repository.ErrOrderNotFound) {
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
