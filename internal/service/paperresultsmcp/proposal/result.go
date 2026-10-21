package proposal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Result is what became of a proposal (issue #829): the records the
// application linked to it (#828) read back in one place, proposal -> trade
// signal -> paper order -> position, so the plan can be checked against the
// outcome. A part is nil until the application has created it (a rejected
// proposal may have a signal and nothing else).
type Result struct {
	Signal   *SignalResult   `json:"signal"`
	Order    *OrderResult    `json:"order"`
	Position *PositionResult `json:"position"`
	MaxLoss  MaxLoss         `json:"max_loss"`
	// StopLossReached is whether the position's closing fill was at or below
	// the proposal's stop_loss_price; TakeProfitReached at or above its
	// take_profit_price. Nil when the proposal has no such price or the
	// position has not closed (a close proposal has neither).
	StopLossReached   *bool `json:"stop_loss_reached"`
	TakeProfitReached *bool `json:"take_profit_reached"`
}

// SignalResult is the trade_signals row the application wrote for the
// proposal. RejectCode is the Risk Engine's fixed code (the reason's text is
// not returned), nil for a signal that passed.
type SignalResult struct {
	ID            int64   `json:"id"`
	At            string  `json:"at"`
	PolicyVersion string  `json:"policy_version"`
	RiskPassed    bool    `json:"risk_passed"`
	RejectCode    *string `json:"reject_code"`
}

// OrderResult is the paper_orders row: the entry order of an open_long
// proposal, the exit order of a close.
type OrderResult struct {
	ID          int64    `json:"id"`
	Side        string   `json:"side"`
	OrderType   string   `json:"order_type"`
	Status      string   `json:"status"`
	Quantity    int64    `json:"quantity"`
	SubmittedAt string   `json:"submitted_at"`
	FilledAt    *string  `json:"filled_at"`
	FilledPrice *float64 `json:"filled_price"`
	FeesJPY     float64  `json:"fees_jpy"`
}

// PositionResult is the positions row. Status is "open" or "closed"; the exit
// fields, RealizedPnl (positions.realized_pnl as stored, no recomputation) and
// HoldingMinutes are nil while it is open.
type PositionResult struct {
	ID             int64    `json:"id"`
	Status         string   `json:"status"`
	Quantity       int64    `json:"quantity"`
	EntryPrice     float64  `json:"entry_price"`
	OpenedAt       string   `json:"opened_at"`
	ClosedAt       *string  `json:"closed_at"`
	ExitPrice      *float64 `json:"exit_price"`
	ExitReason     *string  `json:"exit_reason"`
	RealizedPnl    *float64 `json:"realized_pnl"`
	HoldingMinutes *int64   `json:"holding_minutes"`
	// FeesJPY is the entry and exit orders' fees together, like paper_trades.
	FeesJPY *float64 `json:"fees_jpy"`
}

// MaxLoss sets the loss the proposer expected next to the application's own
// estimate and what happened. ProposedJPY is the proposal's max_loss_jpy,
// EstimatedJPY the application's (quantity x (expected entry - effective
// stop), recorded when it ordered), ActualLossJPY the realized loss of the
// closed position: 0 for a gain or flat result, nil while it is open or there
// is no position.
type MaxLoss struct {
	ProposedJPY     *float64 `json:"proposed_jpy"`
	EstimatedJPY    *float64 `json:"estimated_jpy"`
	ActualLossJPY   *float64 `json:"actual_loss_jpy"`
	EffectiveStop   *float64 `json:"effective_stop_loss_price"`
	AppliedQuantity *int64   `json:"applied_quantity"`
}

const (
	positionOpen   = "open"
	positionClosed = "closed"
)

// result reads the records s is linked to.
func result(ctx context.Context, q query.Q, s State) (Result, error) {
	r := Result{MaxLoss: MaxLoss{
		ProposedJPY: s.MaxLossJPY, EstimatedJPY: s.ComputedMaxLossJPY,
		EffectiveStop: s.EffectiveStopLossPrice, AppliedQuantity: s.AppliedQuantity,
	}}
	var err error
	if s.TradeSignalID != nil {
		if r.Signal, err = signal(ctx, q, *s.TradeSignalID); err != nil {
			return Result{}, err
		}
	}
	if s.PaperOrderID != nil {
		if r.Order, err = order(ctx, q, *s.PaperOrderID); err != nil {
			return Result{}, err
		}
	}
	if s.PositionID != nil {
		if r.Position, err = position(ctx, q, *s.PositionID); err != nil {
			return Result{}, err
		}
	}
	if p := r.Position; p != nil && p.RealizedPnl != nil {
		loss := max(0, -*p.RealizedPnl)
		r.MaxLoss.ActualLossJPY = &loss
		if p.ExitPrice != nil {
			r.StopLossReached = reached(s.StopLossPrice, func(stop float64) bool { return *p.ExitPrice <= stop })
			r.TakeProfitReached = reached(s.TakeProfitPrice, func(take float64) bool { return *p.ExitPrice >= take })
		}
	}
	return r, nil
}

func reached(price *float64, test func(float64) bool) *bool {
	if price == nil {
		return nil
	}
	b := test(*price)
	return &b
}

func signal(ctx context.Context, q query.Q, id int64) (*SignalResult, error) {
	var (
		s      SignalResult
		at     string
		passed int
		reason sql.NullString
	)
	err := q.QueryRowContext(ctx, `
SELECT timestamp, policy_version, risk_passed, reject_reason FROM trade_signals WHERE id = ?`, id).
		Scan(&at, &s.PolicyVersion, &passed, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proposal: read trade signal: %w", err)
	}
	s.ID, s.At, s.RiskPassed = id, query.FormatOut(at), passed != 0
	if code := riskRejectCode(reason); code != "" {
		s.RejectCode = &code
	}
	return &s, nil
}

// riskRejectCode is the fixed code of a "risk_engine_rejected: <code>[: detail]"
// reason; a reason of any other shape is "other" and no reason is empty.
func riskRejectCode(reason sql.NullString) string {
	text := strings.TrimSpace(reason.String)
	if text == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(text, "risk_engine_rejected:")
	if !ok {
		return "other"
	}
	code, _, _ := strings.Cut(rest, ":")
	if code = strings.TrimSpace(code); code != "" {
		return code
	}
	return "unknown"
}

func order(ctx context.Context, q query.Q, id int64) (*OrderResult, error) {
	var (
		o           OrderResult
		submitted   string
		filledAt    sql.NullString
		filledPrice sql.NullFloat64
	)
	err := q.QueryRowContext(ctx, `
SELECT side, order_type, status, quantity, submitted_at, filled_at, filled_price, fees
FROM paper_orders WHERE id = ?`, id).
		Scan(&o.Side, &o.OrderType, &o.Status, &o.Quantity, &submitted, &filledAt, &filledPrice, &o.FeesJPY)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proposal: read paper order: %w", err)
	}
	o.ID, o.SubmittedAt, o.FilledPrice = id, query.FormatOut(submitted), floatPtr(filledPrice)
	if filledAt.Valid {
		t := query.FormatOut(filledAt.String)
		o.FilledAt = &t
	}
	return &o, nil
}

func position(ctx context.Context, q query.Q, id int64) (*PositionResult, error) {
	var (
		p                  PositionResult
		opened             string
		closed, reason     sql.NullString
		exitPrice, pnl, fx sql.NullFloat64
	)
	err := q.QueryRowContext(ctx, `
SELECT p.quantity, p.entry_price, p.opened_at, p.closed_at, p.exit_reason, p.realized_pnl, x.filled_price,
	CASE WHEN e.id IS NULL AND x.id IS NULL THEN NULL
		ELSE COALESCE(e.fees, 0) + COALESCE(x.fees, 0) END
FROM positions p
LEFT JOIN paper_orders e ON e.id = p.entry_order_id
LEFT JOIN paper_orders x ON x.id = p.exit_order_id
WHERE p.id = ?`, id).
		Scan(&p.Quantity, &p.EntryPrice, &opened, &closed, &reason, &pnl, &exitPrice, &fx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proposal: read position: %w", err)
	}
	p.ID, p.OpenedAt, p.Status, p.FeesJPY = id, query.FormatOut(opened), positionOpen, floatPtr(fx)
	if !closed.Valid {
		return &p, nil
	}
	p.Status = positionClosed
	at := query.FormatOut(closed.String)
	p.ClosedAt, p.ExitPrice, p.RealizedPnl = &at, floatPtr(exitPrice), floatPtr(pnl)
	if reason.Valid {
		p.ExitReason = &reason.String
	}
	if minutes, ok := holdingMinutes(opened, closed.String); ok {
		p.HoldingMinutes = &minutes
	}
	return &p, nil
}

// holdingMinutes is the whole minutes between two stored timestamps.
func holdingMinutes(opened, closed string) (int64, bool) {
	from, err := sqlutil.ParseTime(opened)
	if err != nil {
		return 0, false
	}
	to, err := sqlutil.ParseTime(closed)
	if err != nil || to.Before(from) {
		return 0, false
	}
	return int64(to.Sub(from) / time.Minute), true
}
