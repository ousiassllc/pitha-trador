package wsflow_test

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// fakeSymbolProvider is a configurable symbol.SymbolProvider for tests
// .
type fakeSymbolProvider struct {
	state    execution.SymbolState
	stateErr error

	candles    []domain.Snapshot
	candlesErr error
	// lastCandlesFrom/lastCandlesTo capture APICandles' resolved
	// from/to, so tests can assert the default lookback window without
	// re-implementing its calculation.
	lastCandlesFrom time.Time
	lastCandlesTo   time.Time

	position    domain.Position
	positionErr error

	positions []domain.Position

	closeCalls  int
	closeReason string
	closeErr    error
	closeResult domain.Position

	orders []domain.PaperOrder

	decisions []domain.JevDecision
}

func (f *fakeSymbolProvider) State(context.Context, string) (execution.SymbolState, error) {
	return f.state, f.stateErr
}

func (f *fakeSymbolProvider) Candles(_ context.Context, _ string, from, to time.Time) ([]domain.Snapshot, error) {
	f.lastCandlesFrom, f.lastCandlesTo = from, to
	return f.candles, f.candlesErr
}

func (f *fakeSymbolProvider) RecentDecisions(context.Context, string, int) ([]domain.JevDecision, error) {
	return f.decisions, nil
}

func (f *fakeSymbolProvider) GetPosition(context.Context, int64) (domain.Position, error) {
	return f.position, f.positionErr
}

func (f *fakeSymbolProvider) ListPositions(context.Context, int) ([]domain.Position, error) {
	return f.positions, nil
}

func (f *fakeSymbolProvider) CloseAtMarket(_ context.Context, _ int64, reason string, _ time.Time) (domain.Position, error) {
	f.closeCalls++
	f.closeReason = reason
	return f.closeResult, f.closeErr
}

func (f *fakeSymbolProvider) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return f.orders, nil
}

// traderDecision builds a Jev Trader jev_decisions row as enrich.Decision
// leaves it (all six Jev fields populated).
func traderDecision(direction string, confidence float64) domain.JevDecision {
	regime, quality := domain.JevRegimeBreakout, domain.JevEntryQualityStrong
	toxic, stressed := 0.18, 0.09
	return domain.JevDecision{
		DecisionType: domain.JevDecisionTypeTrader, Direction: &direction, Confidence: &confidence,
		Regime: &regime, EntryQuality: &quality, ToxicFlow: &toxic, LiquidityStressed: &stressed,
	}
}

// stateWithTrader is a SymbolState whose latest Trader decision is d.
func stateWithTrader(state execution.SymbolState, d domain.JevDecision) execution.SymbolState {
	state.LatestTraderDecision = &d
	return state
}
