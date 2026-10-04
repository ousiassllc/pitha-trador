package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// SymbolState mirrors functional.md §4.9's per-symbol JSON shape:
//
//	{
//	  "symbol": "XXXX", "last_price": 0, "last_scan_at": null,
//	  "last_jev_scout_at": null, "last_jev_trader_at": null,
//	  "last_signal": "NONE", "last_signal_confidence": 0,
//	  "position": null, "cooldown_until": null
//	}
type SymbolState struct {
	Symbol    string
	LastPrice float64
	// LastVWAP is the latest market_snapshots row's session VWAP
	// (Feature.VWAP), or nil when the symbol has no snapshot yet.
	LastVWAP             *float64
	LastScanAt           *time.Time
	LastJevScoutAt       *time.Time
	LastJevTraderAt      *time.Time
	LastSignal           string
	LastSignalConfidence float64
	// LatestTraderDecision is the symbol's latest Jev Trader decision
	// (enrich.Decision-populated; judgement.DecisionRepository.LatestTrader's
	// definition: no row-count window, no age limit), or nil when Jev Trader
	// has not evaluated it yet. LastJevTraderAt is its Timestamp. Symbol
	// Detail's SSR page, GET /api/v1/symbols/{symbol} and /ws/symbols/{symbol}
	// all read the Jev panel from it, so they agree with the Scanner.
	LatestTraderDecision *domain.JevDecision
	// Position is the currently open position for this symbol, or nil
	// (functional.md §4.9's "position": null).
	Position *domain.Position
	// CooldownUntil is this symbol's post-loss cooldown end time
	// (Engine.Close's doc comment), or nil when not in cooldown.
	CooldownUntil *time.Time
}

// ErrInstrumentUnknown is returned by State when symbol has no
// instruments row.
var ErrInstrumentUnknown = errors.New("execution: unknown symbol")

// State builds symbol's functional.md §4.9 state view from the latest
// market_snapshots row (last_price/last_scan_at), the latest jev_decisions
// row per decision_type (last_jev_scout_at/last_jev_trader_at and
// LatestTraderDecision; one indexed single-row read each, independent of how
// many rows of the other type follow it), the latest trade_signals row (last_signal/
// last_signal_confidence, defaulting to domain.JevDirectionNone/0 when
// none exists yet), the currently open position (if any), and this
// symbol's in-memory cooldown_until (Engine.Close).
//
// It requires Deps.Instruments/Snapshots/Decisions/Signals to have been
// set on NewEngine (returns an error naming the missing one otherwise) -
// unlike EvaluateExit's per-condition nil-tolerance, a Symbol Detail
// state view with silently-missing sections would misrepresent this
// symbol's data availability rather than degrade gracefully.
func (e *Engine) State(ctx context.Context, symbol string) (SymbolState, error) {
	if e.instruments == nil || e.snapshots == nil || e.decisions == nil || e.signals == nil {
		return SymbolState{}, fmt.Errorf("execution: State requires Deps.Instruments/Snapshots/Decisions/Signals to be configured")
	}

	inst, err := e.instruments.GetBySymbol(ctx, symbol)
	if err != nil {
		if errors.Is(err, market.ErrInstrumentNotFound) {
			return SymbolState{}, fmt.Errorf("%w: %s", ErrInstrumentUnknown, symbol)
		}
		return SymbolState{}, fmt.Errorf("execution: look up instrument %q: %w", symbol, err)
	}

	state := SymbolState{Symbol: symbol, LastSignal: domain.JevDirectionNone}

	snapshots, err := e.snapshots.ListByInstrument(ctx, inst.ID, 1)
	if err != nil {
		return SymbolState{}, fmt.Errorf("execution: latest snapshot for %q: %w", symbol, err)
	}
	if len(snapshots) > 0 {
		state.LastPrice = snapshots[0].Price
		vwap := snapshots[0].Feature.VWAP
		state.LastVWAP = &vwap
		ts := snapshots[0].Timestamp
		state.LastScanAt = &ts
	}

	scout, err := e.decisions.LatestScout(ctx, inst.ID)
	switch {
	case err == nil:
		ts := scout.Timestamp
		state.LastJevScoutAt = &ts
	case errors.Is(err, judgement.ErrDecisionNotFound):
		// no Scout decision yet: LastJevScoutAt stays nil
	default:
		return SymbolState{}, fmt.Errorf("execution: latest scout decision for %q: %w", symbol, err)
	}
	trader, err := e.latestTraderDecision(ctx, inst.ID)
	if err != nil {
		return SymbolState{}, err
	}
	if trader != nil {
		ts := trader.Timestamp
		state.LastJevTraderAt = &ts
		state.LatestTraderDecision = trader
	}

	signals, err := e.signals.ListByInstrument(ctx, inst.ID, 1)
	if err != nil {
		return SymbolState{}, fmt.Errorf("execution: latest signal for %q: %w", symbol, err)
	}
	if len(signals) > 0 {
		state.LastSignal = signals[0].Direction
		if signals[0].Score != nil {
			state.LastSignalConfidence = *signals[0].Score
		}
	}

	position, err := e.positions.GetOpenByInstrument(ctx, inst.ID)
	switch {
	case err == nil:
		state.Position = &position
	case errors.Is(err, domain.ErrPositionNotFound):
		// no open position: state.Position stays nil
	default:
		return SymbolState{}, fmt.Errorf("execution: open position for %q: %w", symbol, err)
	}

	if until, inCooldown := e.symbolCooldown(symbol, e.cfg.Now()); inCooldown {
		state.CooldownUntil = &until
	}

	return state, nil
}
