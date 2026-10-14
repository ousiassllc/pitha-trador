package decisiontrade_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
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
	if _, err := db.orders.Fill(ctx, entry.ID, 1000, 0, nil, now); err != nil {
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
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// insertTraderDecision inserts a decision_type=trader jev_decisions row
// with confidence 0.82.
func insertTraderDecision(t *testing.T, decisions *judgement.DecisionRepository, instID int64, timestamp time.Time, direction string) domain.JevDecision {
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
	signals   *trading.SignalRepository
	orders    *trading.OrderRepository
	positions *trading.PositionRepository
}

func TestCalibrationRepository_ListDecisionTrades(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	trades := decisiontrade.New(db)
	decisions := judgement.NewDecisionRepository(db)
	seed := &repositoryDB{
		signals: trading.NewSignalRepository(db), orders: trading.NewOrderRepository(db),
		positions: trading.NewPositionRepository(db),
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

func TestRepository_EachClosedInRange(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	trades := decisiontrade.New(db)
	decisions := judgement.NewDecisionRepository(db)
	seed := &repositoryDB{
		signals: trading.NewSignalRepository(db), orders: trading.NewOrderRepository(db),
		positions: trading.NewPositionRepository(db),
	}
	base := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	// Closed at base+1m (signal-linked) and base+1h+1m (manual entry: no
	// signal/decision, still a closed trade).
	linked := insertTraderDecision(t, decisions, inst.ID, base, domain.JevDirectionLong)
	openDecisionTrade(t, seed, linked, true, 500)
	manual := insertTraderDecision(t, decisions, inst.ID, base.Add(time.Hour), domain.JevDirectionLong)
	openDecisionTrade(t, seed, manual, false, -300)

	var got []decisiontrade.ClosedTrade
	collect := func(c decisiontrade.ClosedTrade) error { got = append(got, c); return nil }
	if err := trades.EachClosedInRange(ctx, base, base.Add(2*time.Hour), collect); err != nil {
		t.Fatalf("EachClosedInRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("closed trades = %+v, want both", got)
	}
	first, second := got[0], got[1]
	if first.JevDecisionID == nil || *first.JevDecisionID != linked.ID || first.TradeSignalID == nil ||
		first.DecisionConfidence == nil || *first.DecisionConfidence != 0.82 {
		t.Errorf("linked trade = %+v, want signal/decision/confidence", first)
	}
	if first.ExitOrderID == nil || first.RealizedPnL != 500 || first.ReturnPct != 0.5 {
		t.Errorf("linked trade pnl = %+v, want realized 500, return 0.5%%", first)
	}
	if second.JevDecisionID != nil || second.TradeSignalID != nil || second.RealizedPnL != -300 {
		t.Errorf("manual trade = %+v, want no signal/decision and realized -300", second)
	}

	// Half-open on closed_at: the first trade closed at base+1m.
	got = nil
	if err := trades.EachClosedInRange(ctx, base.Add(time.Minute), base.Add(time.Hour), collect); err != nil {
		t.Fatalf("EachClosedInRange (narrow): %v", err)
	}
	if len(got) != 1 || got[0].PositionID != first.PositionID {
		t.Fatalf("closed in [base+1m, base+1h) = %+v, want only the first (from inclusive, to exclusive)", got)
	}

	// fn's error stops the read and stays matchable.
	stop := errors.New("stop")
	if err := trades.EachClosedInRange(ctx, base, base.Add(2*time.Hour), func(decisiontrade.ClosedTrade) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("err = %v, want it to wrap fn's error", err)
	}
}
