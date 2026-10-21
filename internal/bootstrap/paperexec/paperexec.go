// Package paperexec adapts Execution's Paper Trading engine to Policy
// Engine's signal-execution hook (issue #49). It lives under
// internal/bootstrap because it is composition-root glue between
// internal/service/policy, execution and risk.
package paperexec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// Executor is policy.SignalExecutor over execution.Engine (issue
// #49): every Policy Engine signal that passed Risk Engine opens a Paper
// Trading position at the evaluated snapshot's price.
type Executor struct {
	Engine *execution.Engine
	Risk   *risk.Engine // FR-RISK-1 judgement and FR-ENTRY-3 position sizing, one step with Enter
}

// ExecuteSignal enters signal via a Paper market order (Engine's default
// order type), filled at snap's quote under Execution's fill model (spread,
// slippage, tick grid, 寄り/引け: fillmodel), not at snap.Price. An instrument that already holds an open position or a
// PENDING entry order, or is still in its post-loss cooldown, is skipped
// rather than failing the jev-trader job: these are Execution's own
// per-symbol gates rejecting a repeat entry, not an error in processing this signal. Likewise a signal
// that reaches Execution after the session ended (a job queued before the
// close) is dropped: retrying would be equally invalid. Enter judges the
// session at snap.Timestamp, so the wall clock (Engine Config.Now) must be
// inside the entry session too: a stale bar stamped inside a session cannot
// open a position while the market is closed (issue #685).
func (p Executor) ExecuteSignal(ctx context.Context, signal domain.TradeSignal, snap domain.Snapshot) error {
	if !p.Engine.EntrySessionOpenNow() {
		slog.InfoContext(ctx, "bootstrap: paper entry skipped", "symbol", signal.Symbol, "signal_id", signal.ID, "reason", execution.ErrOutsideTradingSession.Error())
		return nil
	}
	// The order quantity comes from the Risk limits alone (FR-ENTRY-3). The
	// meta-model's Decision.SizeFraction (Kelly-based) is deliberately NOT
	// used here: it is recorded in metalabel_predictions and the logs only, so every take
	// is sized the same whatever its EV. This is fixed until the meta-model's
	// probability calibration is validated on Paper results (issue #781);
	// do not wire it in without that decision being reopened there.
	//
	// The Risk Engine judges the candidate again here, under the lock that
	// also covers Enter (issue #839): the Policy Engine's Check ran before the
	// signal was recorded, and a concurrent entry may have used the room since.
	// The quantity that judgement settled on is the quantity Enter gets; Enter
	// is not sized again (issue #840). The fill price still differs from the
	// judged price by the spread, the slippage assumption and a tick
	// (risk.EntrySize), so the filled notional can be slightly above the
	// judged one while the share count is the same.
	var result execution.EntryResult
	passed, reason, err := p.Risk.CheckThenEnter(ctx, signal.InstrumentID, signal.Direction, func(size risk.EntrySize) error {
		var enterErr error
		result, enterErr = p.Engine.Enter(ctx, execution.EntryRequest{
			Signal:   signal,
			Quantity: size.Quantity,
			Price:    snap.Price,
			Book:     fillmodel.BookOf(snap),
			Now:      snap.Timestamp,
		})
		return enterErr
	})
	if !passed {
		slog.InfoContext(ctx, "bootstrap: paper entry skipped", "symbol", signal.Symbol, "signal_id", signal.ID, "reason", "risk_engine_rejected_at_entry: "+reason)
		return nil
	}
	switch {
	case errors.Is(err, execution.ErrPositionAlreadyOpen), errors.Is(err, execution.ErrPendingOrderExists), errors.Is(err, execution.ErrSymbolInCooldown),
		errors.Is(err, execution.ErrOutsideTradingSession):
		slog.InfoContext(ctx, "bootstrap: paper entry skipped", "symbol", signal.Symbol, "signal_id", signal.ID, "reason", err.Error())
		return nil
	case err != nil:
		return fmt.Errorf("bootstrap: paper entry for signal %d (%q): %w", signal.ID, signal.Symbol, err)
	}
	slog.InfoContext(ctx, "bootstrap: paper entry submitted", "symbol", signal.Symbol, "signal_id", signal.ID,
		"order_id", result.Order.ID, "order_status", result.Order.Status)
	return nil
}
