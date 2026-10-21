package mcpset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// ErrNotCancellable is returned for a proposal that can no longer be
// cancelled (it was filled, closed, rejected, expired, cancelled or failed, or
// the application is processing it right now). Nothing was stored.
var ErrNotCancellable = errors.New("mcpset: proposal is not cancellable")

// CancelRequested is the outcome of RequestCancel: the proposal's state at the
// time of the request, and whether a cancellation had already been requested
// (Replayed true: nothing new was stored). The cancellation itself is the
// application's (issue #828): until it acts, Status is still the state the
// request was made in.
type CancelRequested struct {
	ProposalID        int64  `json:"proposal_id"`
	Status            string `json:"status"`
	CancelRequestedAt string `json:"cancel_requested_at"`
	Replayed          bool   `json:"replayed"`
}

// RequestCancel records a request to cancel proposal id: the proposal's
// cancel_requested_at and one order_proposal_events row (the status does not
// change), in one transaction. It never cancels anything and never touches
// paper_orders: the application notices the request and cancels the proposal
// (and, for a waiting limit order, the order).
//
// It returns proposal.ErrNotFound for an unknown id and ErrNotCancellable when
// the proposal is not received, waiting_session or submitted. A second request
// for the same proposal returns the first one (Replayed) and stores nothing.
func (w *Writer) RequestCancel(ctx context.Context, id int64) (CancelRequested, error) {
	var out CancelRequested
	err := runInTx(ctx, w.db, "proposal cancel transaction", func(tx *sql.Tx) error {
		var (
			status    string
			requested sql.NullString
		)
		err := tx.QueryRowContext(ctx, `SELECT status, cancel_requested_at FROM order_proposals WHERE id = ?`, id).Scan(&status, &requested)
		if errors.Is(err, sql.ErrNoRows) {
			return proposal.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("mcpset: read proposal %d for cancellation: %w", id, err)
		}
		switch status {
		case proposal.StatusReceived, proposal.StatusWaitingSession, proposal.StatusSubmitted:
		default:
			return ErrNotCancellable
		}
		out = CancelRequested{ProposalID: id, Status: status}
		if requested.Valid {
			out.CancelRequestedAt, out.Replayed = query.FormatOut(requested.String), true
			return nil
		}
		at := sqlutil.FormatTime(w.now())
		res, err := tx.ExecContext(ctx,
			`UPDATE order_proposals SET cancel_requested_at = ? WHERE id = ? AND status IN ('received', 'waiting_session', 'submitted')`, at, id)
		if err != nil {
			return fmt.Errorf("mcpset: request cancellation of proposal %d: %w", id, err)
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return ErrNotCancellable
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor)
VALUES (?, ?, ?, ?, 'cancel_requested', ?)`, id, at, status, status, proposal.ActorMCP); err != nil {
			return fmt.Errorf("mcpset: insert cancel request event of proposal %d: %w", id, err)
		}
		out.CancelRequestedAt = query.FormatOut(at)
		return nil
	})
	return out, err
}
