package proposal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// ErrNotFound is returned by Get for an id with no row.
var ErrNotFound = errors.New("proposal: order proposal not found")

// Event is one row of order_proposal_events.
type Event struct {
	At         string  `json:"at"`
	FromStatus *string `json:"from_status"`
	ToStatus   string  `json:"to_status"`
	ReasonCode string  `json:"reason_code"`
	Actor      string  `json:"actor"`
}

// State is the state of one order proposal (order_proposal_get): what
// was proposed, where it stands, the links the application filled in (issue
// #828) and its events, oldest first. request_hash and client are not part of
// it.
type State struct {
	ProposalID      int64    `json:"proposal_id"`
	Status          string   `json:"status"`
	StatusReason    string   `json:"status_reason"`
	ReceivedAt      string   `json:"received_at"`
	UpdatedAt       string   `json:"updated_at"`
	ExpiresAt       string   `json:"expires_at"`
	Symbol          string   `json:"symbol"`
	Bucket          string   `json:"bucket"`
	Action          string   `json:"action"`
	Quantity        *int64   `json:"quantity"`
	BudgetJPY       *float64 `json:"budget_jpy"`
	OrderType       string   `json:"order_type"`
	LimitPrice      *float64 `json:"limit_price"`
	StopLossPrice   *float64 `json:"stop_loss_price"`
	TakeProfitPrice *float64 `json:"take_profit_price"`
	ExitPlan        *string  `json:"exit_plan"`
	MaxLossJPY      *float64 `json:"max_loss_jpy"`
	Rationale       string   `json:"rationale"`
	Sources         []Source `json:"sources"`
	InstrumentID    *int64   `json:"instrument_id"`
	TradeSignalID   *int64   `json:"trade_signal_id"`
	PaperOrderID    *int64   `json:"paper_order_id"`
	PositionID      *int64   `json:"position_id"`
	Events          []Event  `json:"events"`
}

// Get reads one proposal and its events (ErrNotFound when there is no such
// id).
func Get(ctx context.Context, q query.Q, id int64) (State, error) {
	var (
		p                                 State
		received, updated, expires, srcs  string
		qty, instrument, signal, ord, pos sql.NullInt64
		budget, limit, stop, take, loss   sql.NullFloat64
		plan                              sql.NullString
	)
	err := q.QueryRowContext(ctx, `
SELECT id, status, status_reason, received_at, updated_at, expires_at, symbol, bucket, action, quantity, budget_jpy,
       order_type, limit_price, stop_loss_price, take_profit_price, exit_plan, max_loss_jpy, rationale, sources_json,
       instrument_id, trade_signal_id, paper_order_id, position_id
FROM order_proposals WHERE id = ?`, id).Scan(
		&p.ProposalID, &p.Status, &p.StatusReason, &received, &updated, &expires, &p.Symbol, &p.Bucket, &p.Action, &qty, &budget,
		&p.OrderType, &limit, &stop, &take, &plan, &loss, &p.Rationale, &srcs,
		&instrument, &signal, &ord, &pos)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, ErrNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("proposal: read order proposal: %w", err)
	}
	p.ReceivedAt, p.UpdatedAt, p.ExpiresAt = query.FormatOut(received), query.FormatOut(updated), query.FormatOut(expires)
	p.Quantity, p.BudgetJPY, p.LimitPrice = intPtr(qty), floatPtr(budget), floatPtr(limit)
	p.StopLossPrice, p.TakeProfitPrice, p.MaxLossJPY = floatPtr(stop), floatPtr(take), floatPtr(loss)
	p.InstrumentID, p.TradeSignalID, p.PaperOrderID, p.PositionID = intPtr(instrument), intPtr(signal), intPtr(ord), intPtr(pos)
	if plan.Valid {
		p.ExitPlan = &plan.String
	}
	if err := json.Unmarshal([]byte(srcs), &p.Sources); err != nil {
		return State{}, fmt.Errorf("proposal: decode order proposal sources: %w", err)
	}
	if p.Events, err = events(ctx, q, id); err != nil {
		return State{}, err
	}
	return p, nil
}

func events(ctx context.Context, q query.Q, id int64) ([]Event, error) {
	rows, err := q.QueryContext(ctx, `
SELECT at, from_status, to_status, reason_code, actor FROM order_proposal_events WHERE proposal_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("proposal: read order proposal events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := []Event{}
	for rows.Next() {
		var (
			e    Event
			at   string
			from sql.NullString
		)
		if err := rows.Scan(&at, &from, &e.ToStatus, &e.ReasonCode, &e.Actor); err != nil {
			return nil, fmt.Errorf("proposal: scan order proposal event: %w", err)
		}
		e.At = query.FormatOut(at)
		if from.Valid {
			e.FromStatus = &from.String
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	return &n.Float64
}
