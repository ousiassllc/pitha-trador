package rankingwatch

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// heldOrderLimit bounds the pending-order read; far above any realistic
// number of simultaneous Paper orders.
const heldOrderLimit = 500

// Positions lists the open positions (*trading.PositionRepository).
type Positions interface {
	ListOpen(ctx context.Context) ([]domain.Position, error)
}

// Orders lists orders by status (*trading.OrderRepository).
type Orders interface {
	List(ctx context.Context, status string, limit int) ([]domain.PaperOrder, error)
}

// Held reads the symbols that occupy fixed watch slots: open positions and
// pending (注文中) orders.
type Held struct {
	Positions Positions
	Orders    Orders
}

// HeldSymbols returns the symbols with an open position or a pending order,
// open positions first.
func (h Held) HeldSymbols(ctx context.Context) ([]string, error) {
	positions, err := h.Positions.ListOpen(ctx)
	if err != nil {
		return nil, fmt.Errorf("rankingwatch: list open positions: %w", err)
	}
	orders, err := h.Orders.List(ctx, domain.OrderStatusPending, heldOrderLimit)
	if err != nil {
		return nil, fmt.Errorf("rankingwatch: list pending orders: %w", err)
	}
	symbols := make([]string, 0, len(positions)+len(orders))
	for _, p := range positions {
		symbols = append(symbols, p.Symbol)
	}
	for _, o := range orders {
		symbols = append(symbols, o.Symbol)
	}
	return symbols, nil
}
