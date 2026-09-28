package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// pendingOrderScanLimit bounds how many of an instrument's most recent
// paper_orders rows OnSnapshot scans for PENDING limit entries. Enter
// allows at most one open position per instrument, so only a handful of
// recent orders can ever still be pending.
const pendingOrderScanLimit = 20

// latestTraderDecisionScanLimit is how many recent jev_decisions rows
// OnSnapshot scans for the latest Jev Trader decision (Scout/Trader rows
// interleave per scan cycle; mirrors State's own reasoning).
const latestTraderDecisionScanLimit = 50

// SnapshotResult is OnSnapshot's output: every entry filled and the
// instrument's open position after this market update (nil when none), and
// Exited reports whether that position was closed by an FR-EXIT-1
// condition during this update.
type SnapshotResult struct {
	Filled   []EntryResult
	Position *domain.Position
	Exited   bool
}

// OnSnapshot is Paper Trading's per-market-update step for snap's
// instrument (functional.md §4.8, §4.10 FR-SCHED-4): it fills any PENDING
// limit entry order snap.Price now crosses, marks the open position to
// market, then evaluates FR-EXIT-1's exit conditions against snap and the
// latest Jev Trader decision, closing the position at snap.Price when one
// triggers. It requires Deps.Decisions to have been set on NewEngine.
func (e *Engine) OnSnapshot(ctx context.Context, snap domain.Snapshot) (SnapshotResult, error) {
	if e.decisions == nil {
		return SnapshotResult{}, fmt.Errorf("execution: OnSnapshot requires Deps.Decisions to be configured")
	}
	now := snap.Timestamp

	filled, err := e.fillPendingEntries(ctx, snap)
	if err != nil {
		return SnapshotResult{}, err
	}
	result := SnapshotResult{Filled: filled}

	position, err := e.positions.GetOpenByInstrument(ctx, snap.InstrumentID)
	if errors.Is(err, repository.ErrPositionNotFound) {
		return result, nil
	}
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("execution: open position for %q: %w", snap.Symbol, err)
	}

	unrealized := positionSign(position.Side) * float64(position.Quantity) * (snap.Price - position.EntryPrice)
	position, err = e.positions.Mark(ctx, position.ID, snap.Price, unrealized, now)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("execution: mark position %d to market: %w", position.ID, err)
	}

	decision, err := e.latestTraderDecision(ctx, snap.InstrumentID)
	if err != nil {
		return SnapshotResult{}, err
	}
	mkt := MarketContext{Price: snap.Price, Decision: decision, Now: now}
	if snap.Feature.VWAP > 0 {
		vwap := snap.Feature.VWAP
		mkt.VWAP = &vwap
	}

	reason, exit, err := e.EvaluateExit(ctx, position, mkt)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("execution: evaluate exit for position %d: %w", position.ID, err)
	}
	if !exit {
		result.Position = &position
		return result, nil
	}

	closed, err := e.Close(ctx, position.ID, reason, snap.Price, now)
	if err != nil {
		return SnapshotResult{}, err
	}
	result.Position = &closed
	result.Exited = true
	return result, nil
}

// fillPendingEntries attempts TryFillPending on every PENDING limit entry
// order of snap's instrument.
func (e *Engine) fillPendingEntries(ctx context.Context, snap domain.Snapshot) ([]EntryResult, error) {
	orders, err := e.orders.ListByInstrument(ctx, snap.InstrumentID, pendingOrderScanLimit)
	if err != nil {
		return nil, fmt.Errorf("execution: list orders for %q: %w", snap.Symbol, err)
	}

	var filled []EntryResult
	for _, order := range orders {
		if order.Status != domain.OrderStatusPending || order.OrderType != domain.OrderTypeLimit {
			continue
		}
		direction := domain.JevDirectionLong
		if order.Side == domain.OrderSideSell {
			direction = domain.JevDirectionShort
		}
		entry, ok, err := e.TryFillPending(ctx, order.ID, direction, snap.Price, snap.Timestamp)
		if err != nil {
			return nil, err
		}
		if ok {
			filled = append(filled, entry)
		}
	}
	return filled, nil
}

// latestTraderDecision returns instrumentID's most recent Jev Trader
// decision (EnrichDecision-populated, as EvaluateExit requires), or nil
// when none exists - which disables only the two Jev-derived exit
// conditions (FR-EXIT-3).
func (e *Engine) latestTraderDecision(ctx context.Context, instrumentID int64) (*domain.JevDecision, error) {
	decisions, err := e.decisions.ListByInstrument(ctx, instrumentID, latestTraderDecisionScanLimit)
	if err != nil {
		return nil, fmt.Errorf("execution: recent decisions for instrument %d: %w", instrumentID, err)
	}
	for _, d := range decisions {
		if d.DecisionType == domain.JevDecisionTypeTrader {
			enriched := EnrichDecision(d)
			return &enriched, nil
		}
	}
	return nil, nil
}
