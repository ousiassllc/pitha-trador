package insight_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
)

type fakeDecisions struct{ err error }

func (f fakeDecisions) RecentDecisions(context.Context, string, int) ([]domain.JevDecision, error) {
	return []domain.JevDecision{{ID: 1, Symbol: "7203"}}, f.err
}

type readerFixture struct {
	reader       *insight.Reader
	signals      *repository.SignalRepository
	positions    *repository.PositionRepository
	orders       *repository.OrderRepository
	instrumentID int64
}

func newReaderFixture(t *testing.T) readerFixture {
	t.Helper()
	db, err := repository.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	signals := repository.NewSignalRepository(db)
	positions := repository.NewPositionRepository(db)
	orders := repository.NewOrderRepository(db)
	return readerFixture{
		reader:  insight.NewReader(fakeDecisions{}, instruments, signals, positions),
		signals: signals, positions: positions, orders: orders, instrumentID: inst.ID,
	}
}

func TestReader_RecentSignals_NewestFirstAndUnknownSymbol(t *testing.T) {
	f := newReaderFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_, err := f.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: f.instrumentID, Symbol: "7203", Timestamp: base.Add(time.Duration(i) * time.Minute),
			Direction: domain.JevDirectionNone, PolicyVersion: "v1",
		})
		if err != nil {
			t.Fatalf("seed signal: %v", err)
		}
	}

	got, err := f.reader.RecentSignals(ctx, "7203", 2)
	if err != nil {
		t.Fatalf("RecentSignals: %v", err)
	}
	if len(got) != 2 || !got[0].Timestamp.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("RecentSignals() = %+v, want the 2 most recent, newest first", got)
	}

	if _, err := f.reader.RecentSignals(ctx, "9999", 10); !errors.Is(err, execution.ErrInstrumentUnknown) {
		t.Fatalf("RecentSignals(unknown symbol) error = %v, want ErrInstrumentUnknown", err)
	}
}

func TestReader_Performance_AggregatesClosedPositionsAndDirectionalSignals(t *testing.T) {
	f := newReaderFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	for _, dir := range []string{domain.JevDirectionLong, domain.JevDirectionNone} {
		if _, err := f.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: f.instrumentID, Symbol: "7203", Timestamp: now, Direction: dir, PolicyVersion: "v1",
		}); err != nil {
			t.Fatalf("seed signal: %v", err)
		}
	}
	newOrder := func(side string) int64 {
		t.Helper()
		o, err := f.orders.Insert(ctx, domain.PaperOrder{
			InstrumentID: f.instrumentID, Symbol: "7203", Side: side, OrderType: domain.OrderTypeMarket,
			Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
		})
		if err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return o.ID
	}
	pos, err := f.positions.Open(ctx, domain.Position{
		InstrumentID: f.instrumentID, EntryOrderID: newOrder(domain.OrderSideBuy), Symbol: "7203",
		Side: domain.PositionSideLong, Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: now,
	})
	if err != nil {
		t.Fatalf("seed position: %v", err)
	}
	if _, err := f.positions.Close(ctx, pos.ID, newOrder(domain.OrderSideSell), 2121, 2100, domain.ExitReasonTakeProfit, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("close position: %v", err)
	}

	got, err := f.reader.Performance(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Performance: %v", err)
	}
	if got.TradeCount != 1 || got.WinRate != 1 || got.SignalCount != 1 {
		t.Fatalf("Performance() = %+v, want 1 winning trade and 1 directional signal", got)
	}
	approx(t, "AverageHoldTimeMinutes", got.AverageHoldTimeMinutes, 10)
}

func TestReader_RecentDecisions_DelegatesToSource(t *testing.T) {
	f := newReaderFixture(t)
	got, err := f.reader.RecentDecisions(context.Background(), "7203", 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("RecentDecisions = %+v, %v, want the source's decisions", got, err)
	}
}
