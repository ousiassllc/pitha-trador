// Package latestdecision_test holds the issues #496/#497/#499 regression
// tests: "the latest Jev Trader decision" has no row-count window and no age
// limit, for the repository API (DecisionRepository.LatestTrader /
// LatestTraderByInstruments / LatestScout) and for execution.Engine.State
// (which feeds Symbol Detail SSR, GET /api/v1/symbols/{symbol} and
// /ws/symbols/{symbol}). It lives in its own directory to keep
// internal/service/execution and internal/repository/judgement within the
// per-directory line budget.
package latestdecision_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

type fixture struct {
	engine    *execution.Engine
	decisions *judgement.DecisionRepository
	inst      domain.Instrument
}

func newFixture(t *testing.T, symbol string) fixture {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	instruments := market.NewInstrumentRepository(conn)
	inst, err := instruments.Create(context.Background(), domain.Instrument{Symbol: symbol, Name: symbol, Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	decisions := judgement.NewDecisionRepository(conn)
	engine := execution.NewEngine(execution.Deps{
		Orders: trading.NewOrderRepository(conn), Positions: trading.NewPositionRepository(conn),
		Snapshots: market.NewSnapshotRepository(conn), Decisions: decisions,
		Signals: trading.NewSignalRepository(conn), Instruments: instruments,
	}, execution.Config{})
	return fixture{engine: engine, decisions: decisions, inst: inst}
}

func (f fixture) insert(t *testing.T, decisionType string, at time.Time) domain.JevDecision {
	t.Helper()
	long, confidence := domain.JevDirectionLong, 0.8
	d, err := f.decisions.Insert(context.Background(), domain.JevDecision{
		InstrumentID: f.inst.ID, Symbol: f.inst.Symbol, Timestamp: at, DecisionType: decisionType,
		StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
		Direction: &long, Confidence: &confidence,
	})
	if err != nil {
		t.Fatalf("insert %s decision: %v", decisionType, err)
	}
	return d
}

var base = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

// A Trader decision followed by 60 Scout rows (> the 50-row history window)
// is still the latest Trader decision, however old it is.
func TestLatestTrader_SurvivesLongScoutRunAndIsSharedByStateAndBatch(t *testing.T) {
	f := newFixture(t, "7203")
	ctx := context.Background()
	trader := f.insert(t, domain.JevDecisionTypeTrader, base)
	var lastScout domain.JevDecision
	for i := 1; i <= 60; i++ {
		lastScout = f.insert(t, domain.JevDecisionTypeScout, base.Add(time.Duration(i)*time.Minute))
	}

	if got, err := f.decisions.LatestTrader(ctx, f.inst.ID); err != nil || got.ID != trader.ID {
		t.Fatalf("LatestTrader = %+v, %v, want decision %d older than 60 Scout rows", got, err, trader.ID)
	}
	if batch, err := f.decisions.LatestTraderByInstruments(ctx, []int64{f.inst.ID}); err != nil || batch[f.inst.ID].ID != trader.ID {
		t.Fatalf("LatestTraderByInstruments = %+v, %v, want decision %d", batch, err, trader.ID)
	}
	if got, err := f.decisions.LatestScout(ctx, f.inst.ID); err != nil || got.ID != lastScout.ID {
		t.Fatalf("LatestScout = %+v, %v, want newest Scout %d", got, err, lastScout.ID)
	}

	state, err := f.engine.State(ctx, "7203")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.LatestTraderDecision == nil || state.LatestTraderDecision.ID != trader.ID {
		t.Fatalf("State().LatestTraderDecision = %+v, want decision %d", state.LatestTraderDecision, trader.ID)
	}
	if state.LastJevTraderAt == nil || !state.LastJevTraderAt.Equal(base) {
		t.Fatalf("State().LastJevTraderAt = %v, want %v", state.LastJevTraderAt, base)
	}
	if state.LastJevScoutAt == nil || !state.LastJevScoutAt.Equal(lastScout.Timestamp) {
		t.Fatalf("State().LastJevScoutAt = %v, want %v", state.LastJevScoutAt, lastScout.Timestamp)
	}
}

func TestLatestTrader_TieOnTimestampPrefersLargerID(t *testing.T) {
	f := newFixture(t, "7203")
	f.insert(t, domain.JevDecisionTypeTrader, base)
	second := f.insert(t, domain.JevDecisionTypeTrader, base)

	if got, err := f.decisions.LatestTrader(context.Background(), f.inst.ID); err != nil || got.ID != second.ID {
		t.Fatalf("LatestTrader = %+v, %v, want the larger id %d on equal timestamps", got, err, second.ID)
	}
}

func TestLatestTraderAndScout_NotFoundWhenTypeAbsent(t *testing.T) {
	f := newFixture(t, "7203")
	ctx := context.Background()
	f.insert(t, domain.JevDecisionTypeScout, base)

	if _, err := f.decisions.LatestTrader(ctx, f.inst.ID); !errors.Is(err, judgement.ErrDecisionNotFound) {
		t.Fatalf("LatestTrader(scout-only) error = %v, want ErrDecisionNotFound", err)
	}
	state, err := f.engine.State(ctx, "7203")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.LatestTraderDecision != nil || state.LastJevTraderAt != nil || state.LastJevScoutAt == nil {
		t.Fatalf("State() trader/scout = %+v/%v/%v, want nil trader and a scout time", state.LatestTraderDecision, state.LastJevTraderAt, state.LastJevScoutAt)
	}
}
