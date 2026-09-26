package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// Candles returns symbol's market_snapshots rows in [from, to), ascending
// by timestamp, for `GET /api/v1/symbols/{symbol}/candles`
// (docs/api/endpoints.md §5). It resolves symbol to an instrument the
// same way State does.
func (e *Engine) Candles(ctx context.Context, symbol string, from, to time.Time) ([]domain.Snapshot, error) {
	if e.instruments == nil || e.snapshots == nil {
		return nil, fmt.Errorf("execution: Candles requires Deps.Instruments/Snapshots to be configured")
	}

	inst, err := e.instruments.GetBySymbol(ctx, symbol)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrInstrumentUnknown, symbol)
		}
		return nil, fmt.Errorf("execution: look up instrument %q: %w", symbol, err)
	}

	snapshots, err := e.snapshots.ListByInstrumentRange(ctx, inst.ID, from, to)
	if err != nil {
		return nil, fmt.Errorf("execution: candles for %q: %w", symbol, err)
	}
	return snapshots, nil
}

// GetPosition returns the positions row with the given id
// (repository.PositionRepository.Get), for `POST /positions/:id/close`
// to read the position it is about to close.
func (e *Engine) GetPosition(ctx context.Context, id int64) (domain.Position, error) {
	return e.positions.Get(ctx, id)
}

// ListPositions returns up to limit positions rows, most recently opened
// first (repository.PositionRepository.List), for `GET
// /api/v1/positions`.
func (e *Engine) ListPositions(ctx context.Context, limit int) ([]domain.Position, error) {
	return e.positions.List(ctx, limit)
}

// ListOrders returns up to limit paper_orders rows, optionally filtered
// to status (repository.OrderRepository.List), for `GET /api/v1/orders`.
func (e *Engine) ListOrders(ctx context.Context, status string, limit int) ([]domain.PaperOrder, error) {
	return e.orders.List(ctx, status, limit)
}
