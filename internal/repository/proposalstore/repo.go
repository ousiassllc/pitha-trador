// Package proposalstore is the application's side of order_proposals and
// order_proposal_events (issues #827, #828): what the processor
// (internal/bootstrap/proposalexec) reads and how it records a proposal's
// progress. The MCP's side (inserting a proposal, asking for its
// cancellation) is internal/service/paperresultsmcp/mcpset.
//
// Every status change is one transaction: the order_proposals UPDATE guarded
// by the status it was read at, and one append-only order_proposal_events row.
package proposalstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrStatusChanged is returned by Transition when the proposal is no longer in
// the status the caller read it at (nothing was written): another writer moved
// it first.
var ErrStatusChanged = errors.New("orderproposal: status changed since it was read")

// Repository reads and updates order_proposals for the application.
type Repository struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a Repository over db. now stamps updated_at and the events;
// nil means time.Now.
func New(db *sql.DB, now func() time.Time) *Repository {
	if now == nil {
		now = time.Now
	}
	return &Repository{db: db, now: now}
}

// Links are the columns the application fills in while it processes a
// proposal. A nil field leaves the stored value as it is.
type Links struct {
	InstrumentID  *int64
	TradeSignalID *int64
	PaperOrderID  *int64
	PositionID    *int64
	// AppliedQuantity is the number of shares actually ordered (or closed):
	// the proposal's quantity or the Risk sizing, whichever is smaller.
	AppliedQuantity *int64
	// ComputedMaxLossJPY is the application's own maximum-loss estimate in yen.
	ComputedMaxLossJPY *float64
	// EffectiveStopLossPrice is the stop price that estimate used: the
	// proposal's or the application's, whichever is farther from the entry.
	EffectiveStopLossPrice *float64
}

// Change is one status change of Transition. ToStatus empty keeps the status
// (the event then records from == to, e.g. a quantity reduction).
type Change struct {
	ToStatus string
	// Reason is the fixed reason code: order_proposals.status_reason and the
	// event's reason_code. Empty for a success.
	Reason string
	// DetailJSON is the event's detail_json: a JSON object of codes and
	// numbers, or empty.
	DetailJSON string
	Links      Links
}

const selectColumns = `
SELECT id, symbol, bucket, action, quantity, budget_jpy, order_type, limit_price, stop_loss_price, max_loss_jpy,
       received_at, expires_at, status, status_reason,
       instrument_id, trade_signal_id, paper_order_id, position_id, cancel_requested_at
FROM order_proposals`

// Active lists the proposals the processor still has work for (received,
// waiting for a session, submitted), oldest first.
func (r *Repository) Active(ctx context.Context) ([]orderproposal.Proposal, error) {
	return r.list(ctx, `WHERE status IN ('received', 'waiting_session', 'submitted') ORDER BY received_at, id`)
}

// Processing lists the proposals a previous run left in processing.
func (r *Repository) Processing(ctx context.Context) ([]orderproposal.Proposal, error) {
	return r.list(ctx, `WHERE status = 'processing' ORDER BY received_at, id`)
}

// Get returns the proposal with id, or sql.ErrNoRows.
func (r *Repository) Get(ctx context.Context, id int64) (orderproposal.Proposal, error) {
	return scan(r.db.QueryRowContext(ctx, selectColumns+` WHERE id = ?`, id))
}

func (r *Repository) list(ctx context.Context, tail string) ([]orderproposal.Proposal, error) {
	rows, err := r.db.QueryContext(ctx, selectColumns+` `+tail)
	if err != nil {
		return nil, fmt.Errorf("orderproposal: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []orderproposal.Proposal
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Transition moves proposal id from status from to c.ToStatus (or records an
// event in place), in one transaction: the guarded UPDATE and the event row.
// It returns ErrStatusChanged, writing nothing, when the proposal is not in
// from any more.
func (r *Repository) Transition(ctx context.Context, id int64, from string, c Change) error {
	to := c.ToStatus
	if to == "" {
		to = from
	}
	at := sqlutil.FormatTime(r.now())
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("orderproposal: begin transition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
UPDATE order_proposals SET status = ?, status_reason = ?, updated_at = ?, `+linkAssignments+` WHERE id = ? AND status = ?`,
		append([]any{to, c.Reason, at}, append(c.Links.args(), id, from)...)...)
	if err != nil {
		return fmt.Errorf("orderproposal: update proposal %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("orderproposal: update proposal %d: %w", id, err)
	} else if n == 0 {
		return ErrStatusChanged
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor, detail_json)
VALUES (?, ?, ?, ?, ?, ?, ?)`, id, at, from, to, c.Reason, orderproposal.ActorApp, c.DetailJSON); err != nil {
		return fmt.Errorf("orderproposal: insert event of proposal %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("orderproposal: commit transition of proposal %d: %w", id, err)
	}
	return nil
}

// Link records l on proposal id without changing its status and without an
// event: the trade signal and the plan are written before the order goes out,
// so a restart can find what was already done.
func (r *Repository) Link(ctx context.Context, id int64, l Links) error {
	_, err := r.db.ExecContext(ctx, `UPDATE order_proposals SET `+linkAssignments+` WHERE id = ?`,
		append(l.args(), id)...)
	if err != nil {
		return fmt.Errorf("orderproposal: link proposal %d: %w", id, err)
	}
	return nil
}

// linkAssignments sets every Links column to its new value, or leaves it when
// the argument is NULL.
const linkAssignments = `
    instrument_id = COALESCE(?, instrument_id), trade_signal_id = COALESCE(?, trade_signal_id),
    paper_order_id = COALESCE(?, paper_order_id), position_id = COALESCE(?, position_id),
    applied_quantity = COALESCE(?, applied_quantity), computed_max_loss_jpy = COALESCE(?, computed_max_loss_jpy),
    effective_stop_loss_price = COALESCE(?, effective_stop_loss_price)`

func (l Links) args() []any {
	return []any{
		sqlutil.NullableInt64(l.InstrumentID), sqlutil.NullableInt64(l.TradeSignalID),
		sqlutil.NullableInt64(l.PaperOrderID), sqlutil.NullableInt64(l.PositionID),
		sqlutil.NullableInt64(l.AppliedQuantity), sqlutil.NullableFloat64(l.ComputedMaxLossJPY),
		sqlutil.NullableFloat64(l.EffectiveStopLossPrice),
	}
}

func scan(row sqlutil.RowScanner) (orderproposal.Proposal, error) {
	var (
		p                            orderproposal.Proposal
		qty                          sql.NullInt64
		budget, limit, stop, maxLoss sql.NullFloat64
		instrument, signal           sql.NullInt64
		order, position              sql.NullInt64
		received, expires            string
		cancelRequested              sql.NullString
	)
	if err := row.Scan(&p.ID, &p.Symbol, &p.Bucket, &p.Action, &qty, &budget, &p.OrderType, &limit, &stop, &maxLoss,
		&received, &expires, &p.Status, &p.StatusReason, &instrument, &signal, &order, &position, &cancelRequested); err != nil {
		return orderproposal.Proposal{}, err
	}
	var err error
	if p.ReceivedAt, err = sqlutil.ParseTime(received); err != nil {
		return orderproposal.Proposal{}, fmt.Errorf("orderproposal: proposal %d received_at: %w", p.ID, err)
	}
	if p.ExpiresAt, err = sqlutil.ParseTime(expires); err != nil {
		return orderproposal.Proposal{}, fmt.Errorf("orderproposal: proposal %d expires_at: %w", p.ID, err)
	}
	if cancelRequested.Valid {
		t, err := sqlutil.ParseTime(cancelRequested.String)
		if err != nil {
			return orderproposal.Proposal{}, fmt.Errorf("orderproposal: proposal %d cancel_requested_at: %w", p.ID, err)
		}
		p.CancelRequestedAt = &t
	}
	if qty.Valid {
		p.Quantity = &qty.Int64
	}
	p.BudgetJPY, p.LimitPrice, p.StopLossPrice, p.MaxLossJPY = floatPtr(budget), floatPtr(limit), floatPtr(stop), floatPtr(maxLoss)
	p.InstrumentID, p.TradeSignalID, p.PaperOrderID, p.PositionID = intPtr(instrument), intPtr(signal), intPtr(order), intPtr(position)
	return p, nil
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
