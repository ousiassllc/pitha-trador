package decisiontrade_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
)

// openDecisionTrade seeds decision -> trade_signal -> filled entry order
// -> closed position for one trader decision and returns nothing: the
// caller only checks the aggregation. signalLinked=false leaves the entry
// order without a trade_signal_id (a manual entry).
func openDecisionTrade(t *testing.T, db *repositoryDB, decision domain.JevDecision, signalLinked bool, realizedPnL float64) {
	t.Helper()
	ctx := context.Background()
	now := decision.Timestamp

	var signalID *int64
	if signalLinked {
		sig, err := db.signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: decision.InstrumentID, JevDecisionID: &decision.ID, Symbol: "7203", Timestamp: now,
			Direction: domain.JevDirectionLong, PolicyVersion: "v1", RiskPassed: true,
		})
		if err != nil {
			t.Fatalf("insert signal: %v", err)
		}
		signalID = &sig.ID
	}
	entry, err := db.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: decision.InstrumentID, TradeSignalID: signalID, Symbol: "7203", Side: domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert entry order: %v", err)
	}
	if _, err := db.orders.Fill(ctx, entry.ID, 1000, nil, now); err != nil {
		t.Fatalf("fill entry order: %v", err)
	}
	pos, err := db.positions.Open(ctx, domain.Position{
		InstrumentID: decision.InstrumentID, EntryOrderID: entry.ID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 1000, CurrentPrice: 1000, OpenedAt: now,
	})
	if err != nil {
		t.Fatalf("open position: %v", err)
	}
	exit, err := db.orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: decision.InstrumentID, Symbol: "7203", Side: domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket, Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}
	if _, err := db.positions.Close(ctx, pos.ID, exit.ID, 1000+realizedPnL/100, realizedPnL, domain.ExitReasonManual, now.Add(time.Minute)); err != nil {
		t.Fatalf("close position: %v", err)
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := repository.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// insertTraderDecision inserts a decision_type=trader jev_decisions row
// with confidence 0.82.
func insertTraderDecision(t *testing.T, decisions *repository.DecisionRepository, instID int64, timestamp time.Time, direction string) domain.JevDecision {
	t.Helper()
	confidence := 0.82
	saved, err := decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: instID, Symbol: "7203", Timestamp: timestamp, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}",
		Direction: &direction, Confidence: &confidence, ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("insert trader decision: %v", err)
	}
	return saved
}

type repositoryDB struct {
	signals   *repository.SignalRepository
	orders    *repository.OrderRepository
	positions *repository.PositionRepository
}

func TestCalibrationRepository_ListDecisionTrades(t *testing.T) {
	db := newTestDB(t)
	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	trades := decisiontrade.New(db)
	decisions := repository.NewDecisionRepository(db)
	seed := &repositoryDB{
		signals: repository.NewSignalRepository(db), orders: repository.NewOrderRepository(db),
		positions: repository.NewPositionRepository(db),
	}
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	linked := insertTraderDecision(t, decisions, inst.ID, base, domain.JevDirectionLong)
	openDecisionTrade(t, seed, linked, true, 500)
	manual := insertTraderDecision(t, decisions, inst.ID, base.Add(time.Hour), domain.JevDirectionLong)
	openDecisionTrade(t, seed, manual, false, 700)

	got, err := trades.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List() = %+v, want only the signal-linked closed position", got)
	}
	trade := got[0]
	// insertTraderDecision fixes confidence at 0.82; notional = 1000*100.
	if trade.Confidence != 0.82 || trade.RealizedPnL != 500 || trade.ReturnPct != 0.5 {
		t.Fatalf("trade = %+v, want confidence 0.82, realized 500, return 0.5%%", trade)
	}
}
