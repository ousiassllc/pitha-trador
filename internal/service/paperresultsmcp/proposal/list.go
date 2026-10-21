package proposal

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// ListFilter narrows order_proposals_list. Range is on received_at; an empty
// Status or Bucket matches every proposal. A zero Limit is not valid.
type ListFilter struct {
	Range  query.Range
	Status string
	Bucket string
	Limit  int
	Cursor string
}

// ValidStatus reports whether status is one of the fixed status codes.
func ValidStatus(status string) bool {
	for _, s := range orderproposal.Statuses() {
		if s == status {
			return true
		}
	}
	return false
}

// Item is one order_proposals_list row: the proposal in brief and, when the
// application linked a position to it, that position's realized result. The
// free text (rationale, exit_plan, sources) is in order_proposal_get.
type Item struct {
	ProposalID         int64    `json:"proposal_id"`
	Status             string   `json:"status"`
	StatusReason       string   `json:"status_reason"`
	ReceivedAt         string   `json:"received_at"`
	UpdatedAt          string   `json:"updated_at"`
	ExpiresAt          string   `json:"expires_at"`
	Symbol             string   `json:"symbol"`
	Bucket             string   `json:"bucket"`
	Action             string   `json:"action"`
	OrderType          string   `json:"order_type"`
	Quantity           *int64   `json:"quantity"`
	BudgetJPY          *float64 `json:"budget_jpy"`
	StopLossPrice      *float64 `json:"stop_loss_price"`
	MaxLossJPY         *float64 `json:"max_loss_jpy"`
	AppliedQuantity    *int64   `json:"applied_quantity"`
	ComputedMaxLossJPY *float64 `json:"computed_max_loss_jpy"`
	PositionID         *int64   `json:"position_id"`
	// RealizedPnlJPY is positions.realized_pnl once the linked position has
	// closed, nil before.
	RealizedPnlJPY *float64 `json:"realized_pnl_jpy"`
}

// List returns the proposals of f, newest received first, and the cursor of
// the next page ("" when this is the last).
func List(ctx context.Context, q query.Q, f ListFilter) ([]Item, string, error) {
	from, to := f.Range.Bounds()
	where, args := "o.received_at >= ? AND o.received_at < ?", []any{from, to}
	if f.Status != "" {
		where += " AND o.status = ?"
		args = append(args, f.Status)
	}
	if f.Bucket != "" {
		where += " AND o.bucket = ?"
		args = append(args, f.Bucket)
	}
	if f.Cursor != "" {
		at, id, err := query.DecodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		where += " AND (o.received_at < ? OR (o.received_at = ? AND o.id < ?))"
		args = append(args, at, at, id)
	}
	rows, err := q.QueryContext(ctx, `
SELECT o.id, o.status, o.status_reason, o.received_at, o.updated_at, o.expires_at, o.symbol, o.bucket, o.action,
       o.order_type, o.quantity, o.budget_jpy, o.stop_loss_price, o.max_loss_jpy, o.applied_quantity,
       o.computed_max_loss_jpy, o.position_id, CASE WHEN p.closed_at IS NULL THEN NULL ELSE p.realized_pnl END
FROM order_proposals o
LEFT JOIN positions p ON p.id = o.position_id
WHERE `+where+`
ORDER BY o.received_at DESC, o.id DESC
LIMIT ?`, append(args, f.Limit+1)...)
	if err != nil {
		return nil, "", fmt.Errorf("proposal: list order proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []Item{}
	var lastReceived string
	for rows.Next() {
		var (
			it                              Item
			received, updated, expires      string
			qty, applied, position          sql.NullInt64
			budget, stop, maxLoss, est, pnl sql.NullFloat64
		)
		if err := rows.Scan(&it.ProposalID, &it.Status, &it.StatusReason, &received, &updated, &expires, &it.Symbol,
			&it.Bucket, &it.Action, &it.OrderType, &qty, &budget, &stop, &maxLoss, &applied, &est, &position, &pnl); err != nil {
			return nil, "", fmt.Errorf("proposal: scan order proposal: %w", err)
		}
		if len(items) == f.Limit {
			// The extra row only proves there is a next page; the cursor
			// resumes after the last row returned.
			return items, query.EncodeCursor(lastReceived, items[f.Limit-1].ProposalID), rows.Err()
		}
		it.ReceivedAt, it.UpdatedAt, it.ExpiresAt = query.FormatOut(received), query.FormatOut(updated), query.FormatOut(expires)
		it.Quantity, it.AppliedQuantity, it.PositionID = intPtr(qty), intPtr(applied), intPtr(position)
		it.BudgetJPY, it.StopLossPrice, it.MaxLossJPY = floatPtr(budget), floatPtr(stop), floatPtr(maxLoss)
		it.ComputedMaxLossJPY, it.RealizedPnlJPY = floatPtr(est), floatPtr(pnl)
		lastReceived = received
		items = append(items, it)
	}
	return items, "", rows.Err()
}
