package proposalstore

import (
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

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
