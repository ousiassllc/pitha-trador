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
	Sizer  *risk.Engine // FR-ENTRY-3 position sizing
}

// ExecuteSignal enters signal via a Paper market order (Engine's default
// order type), filled at snap's quote under Execution's fill model (spread,
// slippage, tick grid, 寄り/引け: fillmodel), not at snap.Price. An instrument that already holds an open position or a
// PENDING entry order, or is still in its post-loss cooldown, is skipped
// rather than failing the jev-trader job: these are Execution's own
// per-symbol gates rejecting a repeat entry, not an error in processing this signal. Likewise a signal
// that reaches Execution after the session ended (a job queued before the
// close) is dropped: retrying would be equally invalid.
func (p Executor) ExecuteSignal(ctx context.Context, signal domain.TradeSignal, snap domain.Snapshot) error {
	quantity := p.Sizer.PositionSize(ctx, snap.Price)
	if quantity <= 0 {
		return nil // no lot fits the risk limits (PositionSize logged why)
	}
	result, err := p.Engine.Enter(ctx, execution.EntryRequest{
		Signal:   signal,
		Quantity: quantity,
		Price:    snap.Price,
		Book:     fillmodel.BookOf(snap),
		Now:      snap.Timestamp,
	})
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
