// Package insight provides the read-only queries behind the JSON API
// routes that summarize what the system has decided and realized:
// `GET /api/v1/symbols/{symbol}/decisions`, `/signals`,
// `/signals/{symbol}` and `/performance` (docs/api/endpoints.md §5).
package insight

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// DecisionSource returns a symbol's most recent jev_decisions, newest
// first, returning an error wrapping execution.ErrInstrumentUnknown for
// an unregistered symbol. internal/service/execution.Engine implements it.
type DecisionSource interface {
	RecentDecisions(ctx context.Context, symbol string, limit int) ([]domain.JevDecision, error)
}

// Reader implements internal/web/insightapi.Provider on top of the
// repositories.
type Reader struct {
	decisions   DecisionSource
	instruments *repository.InstrumentRepository
	signals     *repository.SignalRepository
	positions   *repository.PositionRepository
}

// NewReader returns a Reader. Every argument is required.
func NewReader(decisions DecisionSource, instruments *repository.InstrumentRepository,
	signals *repository.SignalRepository, positions *repository.PositionRepository) *Reader {
	return &Reader{decisions: decisions, instruments: instruments, signals: signals, positions: positions}
}

// RecentDecisions returns up to limit jev_decisions for symbol, newest first.
func (r *Reader) RecentDecisions(ctx context.Context, symbol string, limit int) ([]domain.JevDecision, error) {
	return r.decisions.RecentDecisions(ctx, symbol, limit)
}

// RecentSignals returns up to limit trade_signals for symbol, newest
// first; an error wrapping execution.ErrInstrumentUnknown for an
// unregistered symbol.
func (r *Reader) RecentSignals(ctx context.Context, symbol string, limit int) ([]domain.TradeSignal, error) {
	inst, err := r.instruments.GetBySymbol(ctx, symbol)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil, fmt.Errorf("%w: %s", execution.ErrInstrumentUnknown, symbol)
		}
		return nil, fmt.Errorf("insight: look up instrument %q: %w", symbol, err)
	}
	return r.signals.ListByInstrument(ctx, inst.ID, limit)
}

// ListSignals returns up to limit trade_signals across every symbol,
// newest first.
func (r *Reader) ListSignals(ctx context.Context, limit int) ([]domain.TradeSignal, error) {
	return r.signals.ListRecent(ctx, limit)
}

// Performance aggregates every closed position and the LONG/SHORT signal
// count as of now.
func (r *Reader) Performance(ctx context.Context, now time.Time) (Performance, error) {
	// closed_at is never in the future; the extra day only guards a
	// caller-supplied now behind stored timestamps.
	closed, err := r.positions.ListClosedBetween(ctx, time.Unix(0, 0), now.Add(24*time.Hour))
	if err != nil {
		return Performance{}, fmt.Errorf("insight: performance: %w", err)
	}
	signalCount, err := r.signals.CountDirectional(ctx)
	if err != nil {
		return Performance{}, fmt.Errorf("insight: performance: %w", err)
	}
	return Aggregate(closed, signalCount, now), nil
}
