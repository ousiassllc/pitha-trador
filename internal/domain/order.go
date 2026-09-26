package domain

import "time"

// Paper order side values (docs/architecture/er.md §paper_orders CHECK
// constraint).
const (
	OrderSideBuy  = "BUY"
	OrderSideSell = "SELL"
)

// Paper order type values (functional.md FR-ENTRY-1: 成行/指値の両方を
// 選択可能とする).
const (
	OrderTypeMarket = "MARKET"
	OrderTypeLimit  = "LIMIT"
)

// Paper order status values (docs/architecture/er.md §paper_orders CHECK
// constraint).
const (
	OrderStatusPending   = "PENDING"
	OrderStatusFilled    = "FILLED"
	OrderStatusCancelled = "CANCELLED"
	OrderStatusRejected  = "REJECTED"
)

// PaperOrder mirrors one paper_orders row: a Paper Trading (将来は実発注)
// order/fill (docs/architecture/er.md §paper_orders). Execution
// (internal/service/execution) is the only writer; API/SSR handlers only
// read it back.
type PaperOrder struct {
	ID int64
	// InstrumentID is the instruments row this order trades.
	InstrumentID int64
	// TradeSignalID links this order back to the trade_signals row
	// (Policy Engine decision) that triggered it, when one exists (a
	// manual close via POST /positions/:id/close has no originating
	// signal).
	TradeSignalID *int64
	Symbol        string
	// Side is OrderSideBuy or OrderSideSell.
	Side string
	// OrderType is OrderTypeMarket or OrderTypeLimit (FR-ENTRY-1).
	OrderType string
	Quantity  int64
	// LimitPrice is set only when OrderType == OrderTypeLimit.
	LimitPrice *float64
	// Status is one of the OrderStatus* constants.
	Status      string
	SubmittedAt time.Time
	// FilledAt/FilledPrice are set once Status == OrderStatusFilled.
	FilledAt    *time.Time
	FilledPrice *float64
	Fees        float64
	// SlippageBps is the realized slippage in basis points versus the
	// reference price at submission, set only once filled.
	SlippageBps *float64
	CreatedAt   time.Time
}
