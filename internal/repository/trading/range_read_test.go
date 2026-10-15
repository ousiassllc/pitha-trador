package trading_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
)

func TestEachInRange_HalfOpenAndOrdered(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	instrumentID := insertInstrument(t, db, "7203", "トヨタ自動車")
	orders, signals := trading.NewOrderRepository(db), trading.NewSignalRepository(db)
	base := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)

	// Inserted newest first: reads must come back oldest first.
	for _, at := range []time.Time{base.Add(2 * time.Minute), base.Add(time.Minute), base, base.Add(-time.Minute)} {
		if _, err := orders.Insert(ctx, domain.PaperOrder{
			InstrumentID: instrumentID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
			Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: at,
		}); err != nil {
			t.Fatalf("insert order: %v", err)
		}
		if _, err := signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, Direction: domain.JevDirectionNone, PolicyVersion: "v1",
		}); err != nil {
			t.Fatalf("insert signal: %v", err)
		}
	}

	var gotOrders []time.Time
	if err := orders.EachSubmittedInRange(ctx, base, base.Add(2*time.Minute), func(o domain.PaperOrder) error {
		gotOrders = append(gotOrders, o.SubmittedAt)
		return nil
	}); err != nil {
		t.Fatalf("EachSubmittedInRange: %v", err)
	}
	if len(gotOrders) != 2 || !gotOrders[0].Equal(base) || !gotOrders[1].Equal(base.Add(time.Minute)) {
		t.Fatalf("orders in [base, base+2m) = %v, want base, base+1m (from inclusive, to exclusive, oldest first)", gotOrders)
	}

	var gotSignals []time.Time
	if err := signals.EachInRange(ctx, base, base.Add(2*time.Minute), func(s domain.TradeSignal) error {
		gotSignals = append(gotSignals, s.Timestamp)
		return nil
	}); err != nil {
		t.Fatalf("EachInRange: %v", err)
	}
	if len(gotSignals) != 2 || !gotSignals[0].Equal(base) || !gotSignals[1].Equal(base.Add(time.Minute)) {
		t.Fatalf("signals in [base, base+2m) = %v, want base, base+1m", gotSignals)
	}
}

func TestSignalRepository_EachByDecisionRange_OnlySignalsOfDecisionsInPeriod(t *testing.T) {
	ctx := context.Background()
	f := newSignalRepoFixture(t)
	base := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)
	inRange := f.insertDecision(t, base)
	outOfRange := f.insertDecision(t, base.Add(time.Hour))

	for _, id := range []*int64{&inRange, &outOfRange, nil} {
		if _, err := f.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: f.instrumentID, JevDecisionID: id, Symbol: "7203", Timestamp: base, Direction: domain.JevDirectionNone, PolicyVersion: "v1",
		}); err != nil {
			t.Fatalf("insert signal: %v", err)
		}
	}

	var got []domain.TradeSignal
	if err := f.signals.EachByDecisionRange(ctx, base, base.Add(30*time.Minute), func(s domain.TradeSignal) error {
		got = append(got, s)
		return nil
	}); err != nil {
		t.Fatalf("EachByDecisionRange: %v", err)
	}
	if len(got) != 1 || got[0].JevDecisionID == nil || *got[0].JevDecisionID != inRange {
		t.Fatalf("signals = %+v, want only the one tied to the in-range decision", got)
	}
}
