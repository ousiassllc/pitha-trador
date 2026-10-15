package decisiontrade

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ClosedTrade is one closed Paper position joined back to the order that
// closed it and, when they exist, the trade signal and Jev decision that
// opened it: the 決済・損益 row of the trade-log export (issue #770). A
// manual entry has no TradeSignalID/JevDecisionID, and a position closed
// without an exit order has no ExitPrice.
type ClosedTrade struct {
	PositionID   int64
	InstrumentID int64
	Symbol       string
	// Side is PositionSideLong or PositionSideShort.
	Side     string
	Quantity int64

	EntryOrderID  int64
	ExitOrderID   *int64
	TradeSignalID *int64
	// JevDecisionID is the jev_decisions row the entry signal derived from.
	JevDecisionID *int64
	// DecisionConfidence is that decision's Jev confidence.
	DecisionConfidence *float64

	EntryPrice float64
	// ExitPrice is the exit order's filled price.
	ExitPrice   *float64
	RealizedPnL float64
	// ReturnPct is RealizedPnL over the entry notional (entry_price *
	// quantity), in percent; 0 when the notional is not positive.
	ReturnPct  float64
	ExitReason *string
	OpenedAt   time.Time
	ClosedAt   time.Time
}

// eachClosedInRangeSQL reads closed positions with the exit fill and, through
// LEFT JOINs, the signal and Jev decision behind the entry: a manual entry
// (no signal/decision) is still a closed trade, unlike List's decision-only
// view. Rows with a NULL realized_pnl cannot have been closed through
// PositionRepository.Close and are skipped.
const eachClosedInRangeSQL = `
SELECT p.id, p.instrument_id, p.symbol, p.side, p.quantity,
	p.entry_order_id, p.exit_order_id, s.id, s.jev_decision_id, d.confidence,
	p.entry_price, x.filled_price, p.realized_pnl, p.exit_reason, p.opened_at, p.closed_at
FROM positions p
JOIN paper_orders o ON o.id = p.entry_order_id
LEFT JOIN trade_signals s ON s.id = o.trade_signal_id
LEFT JOIN jev_decisions d ON d.id = s.jev_decision_id
LEFT JOIN paper_orders x ON x.id = p.exit_order_id
WHERE p.closed_at >= ? AND p.closed_at < ? AND p.realized_pnl IS NOT NULL
ORDER BY p.closed_at ASC, p.id ASC`

// EachClosedInRange calls fn for every position closed in [from, to),
// oldest close first: the 決済・損益 rows of the trade-log export (issue
// #770). The first error fn returns stops the read and is returned wrapped.
func (r *Repository) EachClosedInRange(ctx context.Context, from, to time.Time, fn func(ClosedTrade) error) error {
	err := sqlutil.Each(ctx, r.db, scanClosedTrade, fn, eachClosedInRangeSQL,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return fmt.Errorf("decisiontrade: read closed trades in [%s, %s): %w", from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), err)
	}
	return nil
}

func scanClosedTrade(row sqlutil.RowScanner) (ClosedTrade, error) {
	var (
		t                  ClosedTrade
		exitOrderID        sql.NullInt64
		signalID           sql.NullInt64
		decisionID         sql.NullInt64
		confidence         sql.NullFloat64
		exitPrice          sql.NullFloat64
		exitReason         sql.NullString
		openedAt, closedAt string
	)
	err := row.Scan(&t.PositionID, &t.InstrumentID, &t.Symbol, &t.Side, &t.Quantity,
		&t.EntryOrderID, &exitOrderID, &signalID, &decisionID, &confidence,
		&t.EntryPrice, &exitPrice, &t.RealizedPnL, &exitReason, &openedAt, &closedAt)
	if err != nil {
		return ClosedTrade{}, fmt.Errorf("decisiontrade: scan closed trade: %w", err)
	}

	t.ExitOrderID = nullInt(exitOrderID)
	t.TradeSignalID = nullInt(signalID)
	t.JevDecisionID = nullInt(decisionID)
	t.DecisionConfidence = nullFloat(confidence)
	t.ExitPrice = nullFloat(exitPrice)
	if exitReason.Valid {
		t.ExitReason = &exitReason.String
	}
	if notional := t.EntryPrice * float64(t.Quantity); notional > 0 {
		t.ReturnPct = t.RealizedPnL / notional * 100
	}

	if t.OpenedAt, err = sqlutil.ParseTime(openedAt); err != nil {
		return ClosedTrade{}, fmt.Errorf("decisiontrade: parse opened_at of position %d: %w", t.PositionID, err)
	}
	if t.ClosedAt, err = sqlutil.ParseTime(closedAt); err != nil {
		return ClosedTrade{}, fmt.Errorf("decisiontrade: parse closed_at of position %d: %w", t.PositionID, err)
	}
	return t, nil
}

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func nullFloat(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}
