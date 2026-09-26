package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func openTestPositionRepo(t *testing.T) (*repository.PositionRepository, *repository.OrderRepository, int64) {
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
	return repository.NewPositionRepository(db), orders, inst.ID
}

func insertFilledEntryOrder(t *testing.T, orders *repository.OrderRepository, instrumentID int64, now time.Time) domain.PaperOrder {
	t.Helper()
	ctx := context.Background()
	created, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert entry order fixture: %v", err)
	}
	filled, err := orders.Fill(ctx, created.ID, 2100.0, nil, now)
	if err != nil {
		t.Fatalf("fill entry order fixture: %v", err)
	}
	return filled
}

func TestPositionRepository_Open_And_GetOpenByInstrument(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	entry := insertFilledEntryOrder(t, orders, instrumentID, now)

	opened, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID,
		EntryOrderID: entry.ID,
		Symbol:       "7203",
		Side:         domain.PositionSideLong,
		Quantity:     100,
		EntryPrice:   2100.0,
		CurrentPrice: 2100.0,
		OpenedAt:     now,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}
	if !opened.IsOpen() {
		t.Fatalf("Open() = %+v, want IsOpen() true", opened)
	}

	got, err := positions.GetOpenByInstrument(ctx, instrumentID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument: %v", err)
	}
	if got.ID != opened.ID || got.Side != domain.PositionSideLong || got.Quantity != 100 {
		t.Fatalf("GetOpenByInstrument() = %+v, want to match Open() input", got)
	}
}

func TestPositionRepository_Open_RejectsSecondOpenPositionForSameInstrument(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	entry1 := insertFilledEntryOrder(t, orders, instrumentID, now)
	entry2 := insertFilledEntryOrder(t, orders, instrumentID, now)

	if _, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry1.ID, Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	}); err != nil {
		t.Fatalf("Open first position: %v", err)
	}

	if _, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry2.ID, Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 50, EntryPrice: 2110, CurrentPrice: 2110, OpenedAt: now,
	}); err == nil {
		t.Fatalf("Open second open position for the same instrument succeeded, want a unique-index error (positions_open_instrument_uq)")
	}
}

func TestPositionRepository_GetOpenByInstrument_NotFound(t *testing.T) {
	positions, _, instrumentID := openTestPositionRepo(t)

	_, err := positions.GetOpenByInstrument(context.Background(), instrumentID)
	if !errors.Is(err, repository.ErrPositionNotFound) {
		t.Fatalf("GetOpenByInstrument(no open position) error = %v, want ErrPositionNotFound", err)
	}
}

func TestPositionRepository_Mark_UpdatesCurrentPriceAndUnrealizedPnL(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	entry := insertFilledEntryOrder(t, orders, instrumentID, now)

	opened, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	marked, err := positions.Mark(ctx, opened.ID, 2115.0, 1500.0, now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if marked.CurrentPrice != 2115.0 || marked.UnrealizedPnL != 1500.0 {
		t.Fatalf("Mark() = %+v, want CurrentPrice=2115.0 UnrealizedPnL=1500.0", marked)
	}
}

func TestPositionRepository_Close_SetsExitFieldsAndUnsetsOpenState(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	entry := insertFilledEntryOrder(t, orders, instrumentID, now)

	opened, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	exitOrder, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}

	closedAt := now.Add(5 * time.Minute)
	closed, err := positions.Close(ctx, opened.ID, exitOrder.ID, 2088.0, -1200.0, domain.ExitReasonStopLoss, closedAt)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.IsOpen() {
		t.Fatalf("Close() = %+v, want IsOpen() false", closed)
	}
	if closed.RealizedPnL == nil || *closed.RealizedPnL != -1200.0 {
		t.Fatalf("Close().RealizedPnL = %v, want -1200.0", closed.RealizedPnL)
	}
	if closed.ExitReason == nil || *closed.ExitReason != domain.ExitReasonStopLoss {
		t.Fatalf("Close().ExitReason = %v, want %q", closed.ExitReason, domain.ExitReasonStopLoss)
	}
	if closed.ClosedAt == nil || !closed.ClosedAt.Equal(closedAt) {
		t.Fatalf("Close().ClosedAt = %v, want %v", closed.ClosedAt, closedAt)
	}

	// Closing frees the instrument to open a new position
	// (positions_open_instrument_uq only rejects a second concurrently
	// open row).
	if _, err := positions.GetOpenByInstrument(ctx, instrumentID); !errors.Is(err, repository.ErrPositionNotFound) {
		t.Fatalf("GetOpenByInstrument() after Close error = %v, want ErrPositionNotFound", err)
	}
}

func TestPositionRepository_Close_NotFound(t *testing.T) {
	positions, _, _ := openTestPositionRepo(t)

	_, err := positions.Close(context.Background(), 999999, 1, 100, 0, domain.ExitReasonManual, time.Now())
	if !errors.Is(err, repository.ErrPositionNotFound) {
		t.Fatalf("Close(unknown) error = %v, want ErrPositionNotFound", err)
	}
}

func TestPositionRepository_ListOpen_And_List(t *testing.T) {
	positions, orders, instrumentID := openTestPositionRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	entry := insertFilledEntryOrder(t, orders, instrumentID, now)

	opened, err := positions.Open(ctx, domain.Position{
		InstrumentID: instrumentID, EntryOrderID: entry.ID, Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	openList, err := positions.ListOpen(ctx)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(openList) != 1 || openList[0].ID != opened.ID {
		t.Fatalf("ListOpen() = %+v, want exactly the opened position", openList)
	}

	exitOrder, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}
	if _, err := positions.Close(ctx, opened.ID, exitOrder.ID, 2110.0, 1000.0, domain.ExitReasonTakeProfit, now.Add(time.Minute)); err != nil {
		t.Fatalf("Close: %v", err)
	}

	openList, err = positions.ListOpen(ctx)
	if err != nil {
		t.Fatalf("ListOpen after close: %v", err)
	}
	if len(openList) != 0 {
		t.Fatalf("ListOpen() after close = %+v, want empty", openList)
	}

	all, err := positions.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 || all[0].ID != opened.ID {
		t.Fatalf("List() = %+v, want the single (now closed) position", all)
	}
}
