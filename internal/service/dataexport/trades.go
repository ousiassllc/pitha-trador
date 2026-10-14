package dataexport

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
)

// OrderSource streams paper_orders by submission time
// (trading.OrderRepository).
type OrderSource interface {
	EachSubmittedInRange(ctx context.Context, from, to time.Time, fn func(domain.PaperOrder) error) error
}

// SignalSource streams trade_signals by signal time
// (trading.SignalRepository).
type SignalSource interface {
	EachInRange(ctx context.Context, from, to time.Time, fn func(domain.TradeSignal) error) error
}

// ClosedTradeSource streams the positions closed in a period with their exit
// fill and originating signal/decision (decisiontrade.Repository).
type ClosedTradeSource interface {
	EachClosedInRange(ctx context.Context, from, to time.Time, fn func(decisiontrade.ClosedTrade) error) error
}

// Trades is the trade-log Collector (KindTrades): Paper orders and fills
// (orders, the primary table), the trade signals behind them (signals) and
// the positions closed with their realized PnL (closed_positions).
type Trades struct {
	Orders  OrderSource
	Signals SignalSource
	Closed  ClosedTradeSource
}

// Kind implements Collector.
func (Trades) Kind() Kind { return KindTrades }

// Tables implements Collector.
func (t Trades) Tables() []Table {
	return []Table{t.ordersTable(), t.signalsTable(), t.closedTable()}
}

func (t Trades) ordersTable() Table {
	return Table{
		Name: "orders",
		Columns: cols("order_id", "trade_signal_id", "instrument_id", "symbol", "side", "order_type", "quantity",
			"limit_price", "status", "submitted_at", "filled_at", "filled_price", "fees", "slippage_bps"),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return t.Orders.EachSubmittedInRange(ctx, p.From, p.To, func(o domain.PaperOrder) error {
				return emit(Row{o.ID, val(o.TradeSignalID), o.InstrumentID, o.Symbol, o.Side, o.OrderType, o.Quantity,
					val(o.LimitPrice), o.Status, o.SubmittedAt, val(o.FilledAt), val(o.FilledPrice), o.Fees, val(o.SlippageBps)})
			})
		},
	}
}

func (t Trades) signalsTable() Table {
	return Table{
		Name: "signals",
		Columns: cols("signal_id", "decision_id", "instrument_id", "symbol", "timestamp", "direction", "score",
			"entry_price_reference", "policy_version", "risk_passed", "reject_reason"),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return t.Signals.EachInRange(ctx, p.From, p.To, func(s domain.TradeSignal) error {
				return emit(signalRow(s))
			})
		},
	}
}

// signalRow is shared with the decision log's skip/reject table.
func signalRow(s domain.TradeSignal) Row {
	return Row{s.ID, val(s.JevDecisionID), s.InstrumentID, s.Symbol, s.Timestamp, s.Direction, val(s.Score),
		val(s.EntryPriceReference), s.PolicyVersion, s.RiskPassed, val(s.RejectReason)}
}

func (t Trades) closedTable() Table {
	return Table{
		Name: "closed_positions",
		Columns: cols("position_id", "instrument_id", "symbol", "side", "quantity", "entry_order_id", "exit_order_id",
			"trade_signal_id", "decision_id", "decision_confidence", "entry_price", "exit_price", "realized_pnl",
			"return_pct", "exit_reason", "opened_at", "closed_at"),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return t.Closed.EachClosedInRange(ctx, p.From, p.To, func(c decisiontrade.ClosedTrade) error {
				return emit(Row{c.PositionID, c.InstrumentID, c.Symbol, c.Side, c.Quantity, c.EntryOrderID, val(c.ExitOrderID),
					val(c.TradeSignalID), val(c.JevDecisionID), val(c.DecisionConfidence), c.EntryPrice, val(c.ExitPrice),
					c.RealizedPnL, c.ReturnPct, val(c.ExitReason), c.OpenedAt, c.ClosedAt})
			})
		},
	}
}
