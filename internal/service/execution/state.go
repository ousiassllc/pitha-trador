package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
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
	Symbol               string
	LastPrice            float64
	LastScanAt           *time.Time
	LastJevScoutAt       *time.Time
	LastJevTraderAt      *time.Time
	LastSignal           string
	LastSignalConfidence float64
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
// market_snapshots row (last_price/last_scan_at), the latest
// jev_decisions rows per decision_type (last_jev_scout_at/
// last_jev_trader_at), the latest trade_signals row (last_signal/
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
		if errors.Is(err, repository.ErrInstrumentNotFound) {
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
		ts := snapshots[0].Timestamp
		state.LastScanAt = &ts
	}

	// jev_decisions has no per-decision_type "latest" query, so scan
	// enough recent rows for both types (Scout/Trader alternate roughly
	// 1:1 per scan cycle, functional.md §4.3).
	decisions, err := e.decisions.ListByInstrument(ctx, inst.ID, 50)
	if err != nil {
		return SymbolState{}, fmt.Errorf("execution: recent decisions for %q: %w", symbol, err)
	}
	for _, d := range decisions {
		ts := d.Timestamp
		switch d.DecisionType {
		case domain.JevDecisionTypeScout:
			if state.LastJevScoutAt == nil {
				state.LastJevScoutAt = &ts
			}
		case domain.JevDecisionTypeTrader:
			if state.LastJevTraderAt == nil {
				state.LastJevTraderAt = &ts
			}
		}
		if state.LastJevScoutAt != nil && state.LastJevTraderAt != nil {
			break
		}
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
	case errors.Is(err, repository.ErrPositionNotFound):
		// no open position: state.Position stays nil
	default:
		return SymbolState{}, fmt.Errorf("execution: open position for %q: %w", symbol, err)
	}

	if until, inCooldown := e.symbolCooldown(symbol, e.cfg.Now()); inCooldown {
		state.CooldownUntil = &until
	}

	return state, nil
}
