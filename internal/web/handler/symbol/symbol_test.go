package symbol_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/symbol"
)

// fakeSymbolProvider is a configurable symbol.SymbolProvider for tests
// (symbol_detail_test.go, symbol_list_test.go, symbol_close_test.go).
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

func (f *fakeSymbolProvider) Close(_ context.Context, _ int64, reason string, _ float64, _ fillmodel.Book, _ time.Time) (domain.Position, error) {
	f.closeCalls++
	f.closeReason = reason
	return f.closeResult, f.closeErr
}

func (f *fakeSymbolProvider) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return f.orders, nil
}

func TestStaticSymbolProvider_DefaultsAreEmpty(t *testing.T) {
	p := symbol.StaticSymbolProvider{}
	ctx := context.Background()

	state, err := p.State(ctx, "7203")
	if err != nil || state.LastSignal != domain.JevDirectionNone {
		t.Fatalf("State = %+v, %v, want LastSignal=NONE, nil error", state, err)
	}
	if positions, err := p.ListPositions(ctx, 10); err != nil || len(positions) != 0 {
		t.Fatalf("ListPositions = %+v, %v, want empty/nil error", positions, err)
	}
	if orders, err := p.ListOrders(ctx, "", 10); err != nil || len(orders) != 0 {
		t.Fatalf("ListOrders = %+v, %v, want empty/nil error", orders, err)
	}
	if _, err := p.GetPosition(ctx, 1); !errors.Is(err, domain.ErrPositionNotFound) {
		t.Fatalf("GetPosition error = %v, want ErrPositionNotFound", err)
	}
}

func strPtr(s string) *string { return &s }

func TestNewSymbolRiskParams_UsesConfiguredLimitsAndExitRule(t *testing.T) {
	limits := config.RiskLimits{MaxPositionPerSymbolPct: 1.0}
	exit := execution.DefaultConfig()
	exit.StopLossPct = 0.4
	exit.TakeProfitPct = 0.9

	got := symbol.NewSymbolRiskParams(limits, exit, nil)

	want := symbol.SymbolRiskParams{AllowedPositionPct: 1.0, StopLossPct: 0.4, TakeProfitPct: 0.9}
	if got.AllowedPositionPct != want.AllowedPositionPct || got.StopLossPct != want.StopLossPct ||
		got.TakeProfitPct != want.TakeProfitPct || got.AllowedPositionPctFor != nil {
		t.Fatalf("NewSymbolRiskParams() = %+v, want %+v", got, want)
	}
}
