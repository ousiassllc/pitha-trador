package handler

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// SymbolStateProvider is the subset of internal/service/execution.Engine
// the Symbol Detail routes below need for a symbol's current state
// (docs/api/endpoints.md §5 `GET /api/v1/symbols/{symbol}`).
type SymbolStateProvider interface {
	State(ctx context.Context, symbol string) (execution.SymbolState, error)
	Candles(ctx context.Context, symbol string, from, to time.Time) ([]domain.Snapshot, error)
	// RecentDecisions is the Symbol Detail SSR page's "Decision history"
	// source (functional.md §5.2) - a page-rendering read, not a JSON
	// API route (symbol.go's SymbolHandler doc comment).
	RecentDecisions(ctx context.Context, symbol string, limit int) ([]domain.JevDecision, error)
}

// PositionExecutor is the subset of internal/service/execution.Engine the
// position routes below need: reading one position back and closing it
// (`POST /positions/:id/close`, docs/api/endpoints.md §4).
type PositionExecutor interface {
	GetPosition(ctx context.Context, id int64) (domain.Position, error)
	ListPositions(ctx context.Context, limit int) ([]domain.Position, error)
	Close(ctx context.Context, positionID int64, reason string, exitPrice float64, now time.Time) (domain.Position, error)
}

// OrderLister is the subset of internal/service/execution.Engine `GET
// /api/v1/orders` needs.
type OrderLister interface {
	ListOrders(ctx context.Context, status string, limit int) ([]domain.PaperOrder, error)
}

// SymbolProvider is every Symbol Detail/position/order route's combined
// dependency - the interface a single internal/service/execution.Engine
// satisfies in full (issue #37's Enter/Close/State/Candles/List* methods).
type SymbolProvider interface {
	SymbolStateProvider
	PositionExecutor
	OrderLister
}

// StaticSymbolProvider is a fixed, empty SymbolProvider, used as
// internal/router.New()'s default until a real
// internal/service/execution.Engine is wired in (mirrors
// StaticCandidateSource/StaticSystemEngine's role for their own routes).
// Every read returns an empty/zero result; Close always fails, since
// there is never an open position to close against an empty backing
// store.
type StaticSymbolProvider struct{}

func (StaticSymbolProvider) State(_ context.Context, symbol string) (execution.SymbolState, error) {
	return execution.SymbolState{Symbol: symbol, LastSignal: domain.JevDirectionNone}, nil
}

func (StaticSymbolProvider) Candles(context.Context, string, time.Time, time.Time) ([]domain.Snapshot, error) {
	return nil, nil
}

func (StaticSymbolProvider) RecentDecisions(context.Context, string, int) ([]domain.JevDecision, error) {
	return nil, nil
}

func (StaticSymbolProvider) GetPosition(context.Context, int64) (domain.Position, error) {
	return domain.Position{}, domain.ErrPositionNotFound
}

func (StaticSymbolProvider) ListPositions(context.Context, int) ([]domain.Position, error) {
	return nil, nil
}

func (StaticSymbolProvider) Close(context.Context, int64, string, float64, time.Time) (domain.Position, error) {
	return domain.Position{}, domain.ErrPositionNotFound
}

func (StaticSymbolProvider) ListOrders(context.Context, string, int) ([]domain.PaperOrder, error) {
	return nil, nil
}

// SymbolRiskParams is the Risk Engine limit set `GET
// /api/v1/symbols/{symbol}`'s "risk" section reports
// (functional.md §4.7's table, §4.8's FR-EXIT-2 initial values).
// AllowedPositionPct is Risk Engine's max_position_per_symbol_pct limit
// itself (the ceiling every symbol shares), not a computed per-symbol
// remaining headroom - no account-equity/exposure computation exists yet
// outside internal/service/risk's own placeholder PortfolioProvider
// (issue #36's ZeroPortfolioProvider), so reporting a dynamically
// "remaining" value would fabricate data this sub-scope has no source
// for.
type SymbolRiskParams struct {
	AllowedPositionPct float64
	StopLossPct        float64
	TakeProfitPct      float64
}

// SymbolHandler implements the Symbol Detail API/action routes
// (docs/api/endpoints.md §4 `/positions/:id/close`, §5
// `/symbols/{symbol}`, `/symbols/{symbol}/candles`, `/positions`,
// `/orders`). Its route handlers are split across this file (scaffold),
// symbol_detail.go (APISymbol/APICandles), symbol_list.go
// (APIPositions/APIOrders), symbol_close.go (ClosePosition), and
// symbol_ws.go (WebSocket).
type SymbolHandler struct {
	provider     SymbolProvider
	riskParams   SymbolRiskParams
	now          func() time.Time
	tickInterval time.Duration
}

// defaultTickInterval is `/ws/symbols/{symbol}`'s push spacing
// (symbol_ws.go). docs/api/endpoints.md §6 does not specify a cadence
// for tick messages (unlike `/ws/scanner`'s stated 15-30s candidate
// refresh); 2s balances a responsive `pitha-price-chart` against
// needless polling of SymbolProvider.State per connected client.
const defaultTickInterval = 2 * time.Second

// NewSymbolHandler returns a SymbolHandler backed by provider, reporting
// riskParams in every `GET /api/v1/symbols/{symbol}` response.
func NewSymbolHandler(provider SymbolProvider, riskParams SymbolRiskParams) *SymbolHandler {
	return &SymbolHandler{provider: provider, riskParams: riskParams, now: time.Now, tickInterval: defaultTickInterval}
}

// SetTickInterval overrides `/ws/symbols/{symbol}`'s push spacing
// (default defaultTickInterval). Exposed for tests that need a fast
// interval rather than production callers, mirroring
// NewScannerHandler's constructor-supplied CandidateRefreshInterval for
// the same reason.
func (h *SymbolHandler) SetTickInterval(d time.Duration) { h.tickInterval = d }
