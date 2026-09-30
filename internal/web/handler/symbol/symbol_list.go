package symbol

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// positionOutput mirrors one `GET /api/v1/positions` item.
type positionOutput struct {
	ID            int64    `json:"id"`
	Symbol        string   `json:"symbol"`
	Side          string   `json:"side"`
	Quantity      int64    `json:"quantity"`
	EntryPrice    float64  `json:"entry_price"`
	CurrentPrice  float64  `json:"current_price"`
	UnrealizedPnL float64  `json:"unrealized_pnl"`
	RealizedPnL   *float64 `json:"realized_pnl"`
	OpenedAt      string   `json:"opened_at"`
	ClosedAt      *string  `json:"closed_at"`
	ExitReason    *string  `json:"exit_reason"`
}

func toPositionOutput(p domain.Position) positionOutput {
	out := positionOutput{
		ID: p.ID, Symbol: p.Symbol, Side: p.Side, Quantity: p.Quantity,
		EntryPrice: p.EntryPrice, CurrentPrice: p.CurrentPrice, UnrealizedPnL: p.UnrealizedPnL,
		RealizedPnL: p.RealizedPnL, OpenedAt: p.OpenedAt.Format(time.RFC3339), ExitReason: p.ExitReason,
	}
	if p.ClosedAt != nil {
		closedAt := p.ClosedAt.Format(time.RFC3339)
		out.ClosedAt = &closedAt
	}
	return out
}

// PositionsAPIOutput is the Huma response body for `GET
// /api/v1/positions`.
type PositionsAPIOutput struct {
	Body struct {
		Items []positionOutput `json:"items"`
	}
}

// PositionsInput is `GET /api/v1/positions`'s query parameters.
type PositionsInput struct {
	Limit int `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Maximum number of positions to return (1-500)."`
}

// APIPositions implements `GET /api/v1/positions` (docs/api/endpoints.md
// §5): current and recently-closed positions, most recently opened
// first.
func (h *SymbolHandler) APIPositions(ctx context.Context, in *PositionsInput) (*PositionsAPIOutput, error) {
	positions, err := h.provider.ListPositions(ctx, in.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("list positions failed", err)
	}

	out := &PositionsAPIOutput{}
	out.Body.Items = make([]positionOutput, len(positions))
	for i, p := range positions {
		out.Body.Items[i] = toPositionOutput(p)
	}
	return out, nil
}

// orderOutput mirrors one `GET /api/v1/orders` item.
type orderOutput struct {
	ID          int64    `json:"id"`
	Symbol      string   `json:"symbol"`
	Side        string   `json:"side"`
	OrderType   string   `json:"order_type"`
	Quantity    int64    `json:"quantity"`
	LimitPrice  *float64 `json:"limit_price"`
	Status      string   `json:"status"`
	SubmittedAt string   `json:"submitted_at"`
	FilledAt    *string  `json:"filled_at"`
	FilledPrice *float64 `json:"filled_price"`
	Fees        float64  `json:"fees"`
	SlippageBps *float64 `json:"slippage_bps"`
}

// OrdersAPIOutput is the Huma response body for `GET /api/v1/orders`.
type OrdersAPIOutput struct {
	Body struct {
		Items []orderOutput `json:"items"`
	}
}

// OrdersInput is `GET /api/v1/orders`'s query parameters
// (docs/api/endpoints.md §5: "ステータスフィルタ `?status=` 対応").
type OrdersInput struct {
	Status string `query:"status" enum:"PENDING,FILLED,CANCELLED,REJECTED" doc:"Filter by order status. Empty returns every status."`
	Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Maximum number of orders to return (1-500)."`
}

// APIOrders implements `GET /api/v1/orders` (docs/api/endpoints.md §5):
// paper_orders, most recently submitted first, optionally filtered by
// status.
func (h *SymbolHandler) APIOrders(ctx context.Context, in *OrdersInput) (*OrdersAPIOutput, error) {
	orders, err := h.provider.ListOrders(ctx, in.Status, in.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("list orders failed", err)
	}

	out := &OrdersAPIOutput{}
	out.Body.Items = make([]orderOutput, len(orders))
	for i, o := range orders {
		item := orderOutput{
			ID: o.ID, Symbol: o.Symbol, Side: o.Side, OrderType: o.OrderType, Quantity: o.Quantity,
			LimitPrice: o.LimitPrice, Status: o.Status, SubmittedAt: o.SubmittedAt.Format(time.RFC3339),
			FilledPrice: o.FilledPrice, Fees: o.Fees, SlippageBps: o.SlippageBps,
		}
		if o.FilledAt != nil {
			filledAt := o.FilledAt.Format(time.RFC3339)
			item.FilledAt = &filledAt
		}
		out.Body.Items[i] = item
	}
	return out, nil
}
