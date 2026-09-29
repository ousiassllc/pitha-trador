package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// paperExecutor is policy.SignalExecutor over execution.Engine (issue
// #49): every Policy Engine signal that passed Risk Engine opens a Paper
// Trading position at the evaluated snapshot's price.
type paperExecutor struct {
	engine *execution.Engine
	sizer  *risk.Engine // FR-ENTRY-3 position sizing
}

// ExecuteSignal enters signal via a Paper market order (Engine's default
// order type). An instrument that already holds an open position or is
// still in its post-loss cooldown is skipped rather than failing the
// jev-trader job: both are Execution's own per-symbol gates rejecting a
// repeat entry, not an error in processing this signal.
func (p paperExecutor) ExecuteSignal(ctx context.Context, signal domain.TradeSignal, snap domain.Snapshot) error {
	quantity := p.sizer.PositionSize(ctx, snap.Price)
	if quantity <= 0 {
		return nil // no lot fits the risk limits (PositionSize logged why)
	}
	result, err := p.engine.Enter(ctx, execution.EntryRequest{
		Signal:   signal,
		Quantity: quantity,
		Price:    snap.Price,
		Now:      snap.Timestamp,
	})
	switch {
	case errors.Is(err, execution.ErrPositionAlreadyOpen), errors.Is(err, execution.ErrSymbolInCooldown):
		slog.InfoContext(ctx, "bootstrap: paper entry skipped", "symbol", signal.Symbol, "signal_id", signal.ID, "reason", err.Error())
		return nil
	case err != nil:
		return fmt.Errorf("bootstrap: paper entry for signal %d (%q): %w", signal.ID, signal.Symbol, err)
	}
	slog.InfoContext(ctx, "bootstrap: paper entry submitted", "symbol", signal.Symbol, "signal_id", signal.ID,
		"order_id", result.Order.ID, "order_status", result.Order.Status)
	return nil
}
