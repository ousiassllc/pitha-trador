package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// ErrOrderNotFound is returned by OrderRepository methods when no
// matching paper_orders row exists.
var ErrOrderNotFound = errors.New("repository: paper order not found")

// OrderRepository persists paper_orders rows: every Paper Trading (将来
// は実発注) order/fill Execution submits (docs/architecture/er.md
// §paper_orders, functional.md §4.8 Entry/Exit).
type OrderRepository struct {
	db *sql.DB
}

// NewOrderRepository returns an OrderRepository backed by db.
func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

const insertOrderSQL = `
INSERT INTO paper_orders (
	instrument_id, trade_signal_id, symbol, side, order_type, quantity,
	limit_price, status, submitted_at, filled_at, filled_price, fees,
	slippage_bps, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert writes a single paper_orders row.
func (r *OrderRepository) Insert(ctx context.Context, o domain.PaperOrder) (domain.PaperOrder, error) {
	createdAt := o.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	res, err := r.db.ExecContext(ctx, insertOrderSQL,
		o.InstrumentID, nullableInt64(o.TradeSignalID), o.Symbol, o.Side, o.OrderType, o.Quantity,
		nullableFloat64(o.LimitPrice), o.Status, formatTime(o.SubmittedAt), nullableTime(o.FilledAt),
		nullableFloat64(o.FilledPrice), o.Fees, nullableFloat64(o.SlippageBps), formatTime(createdAt),
	)
	if err != nil {
		return domain.PaperOrder{}, fmt.Errorf(
			"repository: insert paper order for instrument %d (%s %s): %w", o.InstrumentID, o.Side, o.OrderType, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.PaperOrder{}, fmt.Errorf("repository: read paper order id for instrument %d: %w", o.InstrumentID, err)
	}

	o.ID = id
	o.CreatedAt = createdAt
	return o, nil
}

// Get returns the paper_orders row with the given id, or
// ErrOrderNotFound.
func (r *OrderRepository) Get(ctx context.Context, id int64) (domain.PaperOrder, error) {
	row := r.db.QueryRowContext(ctx, orderSelectColumns+` WHERE id = ?`, id)
	return scanOrder(row)
}

// Fill marks a PENDING order FILLED at price/now (Paper Entry/Exit
// execution, functional.md FR-ENTRY-1), recording slippageBps versus the
// order's reference price. It returns ErrOrderNotFound if id does not
// exist.
func (r *OrderRepository) Fill(ctx context.Context, id int64, price float64, slippageBps *float64, now time.Time) (domain.PaperOrder, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE paper_orders SET status = ?, filled_at = ?, filled_price = ?, slippage_bps = ? WHERE id = ?`,
		domain.OrderStatusFilled, formatTime(now), price, nullableFloat64(slippageBps), id,
	)
	if err != nil {
		return domain.PaperOrder{}, fmt.Errorf("repository: fill paper order %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.PaperOrder{}, ErrOrderNotFound
	}
	return r.Get(ctx, id)
}

// UpdateStatus sets a paper_orders row's status (e.g. to
// OrderStatusCancelled/OrderStatusRejected). It returns ErrOrderNotFound
// if id does not exist.
func (r *OrderRepository) UpdateStatus(ctx context.Context, id int64, status string) (domain.PaperOrder, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE paper_orders SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return domain.PaperOrder{}, fmt.Errorf("repository: update paper order %d status: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.PaperOrder{}, ErrOrderNotFound
	}
	return r.Get(ctx, id)
}

// ListByInstrument returns up to limit paper_orders rows for
// instrumentID, most recent first (submitted_at desc).
func (r *OrderRepository) ListByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.PaperOrder, error) {
	rows, err := r.db.QueryContext(ctx,
		orderSelectColumns+` WHERE instrument_id = ? ORDER BY submitted_at DESC LIMIT ?`, instrumentID, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list paper orders for instrument %d: %w", instrumentID, err)
	}
	return scanOrders(rows)
}

// List returns up to limit paper_orders rows, most recent first
// (submitted_at desc), optionally filtered to a single status
// (docs/api/endpoints.md §5 `GET /api/v1/orders?status=`). An empty
// status returns every order regardless of status.
func (r *OrderRepository) List(ctx context.Context, status string, limit int) ([]domain.PaperOrder, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = r.db.QueryContext(ctx, orderSelectColumns+` ORDER BY submitted_at DESC LIMIT ?`, limit)
	} else {
		rows, err = r.db.QueryContext(ctx,
			orderSelectColumns+` WHERE status = ? ORDER BY submitted_at DESC LIMIT ?`, status, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("repository: list paper orders (status=%q): %w", status, err)
	}
	return scanOrders(rows)
}

func scanOrders(rows *sql.Rows) ([]domain.PaperOrder, error) {
	defer func() { _ = rows.Close() }()

	var out []domain.PaperOrder
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list paper orders: %w", err)
	}
	return out, nil
}

const orderSelectColumns = `
SELECT id, instrument_id, trade_signal_id, symbol, side, order_type, quantity,
	limit_price, status, submitted_at, filled_at, filled_price, fees,
	slippage_bps, created_at
FROM paper_orders`

func scanOrder(row rowScanner) (domain.PaperOrder, error) {
	var (
		o             domain.PaperOrder
		tradeSignalID *int64
		submittedAt   string
		filledAt      sql.NullString
		createdAt     string
	)

	err := row.Scan(
		&o.ID, &o.InstrumentID, nullInt64(&tradeSignalID), &o.Symbol, &o.Side, &o.OrderType, &o.Quantity,
		nullFloat(&o.LimitPrice), &o.Status, &submittedAt, &filledAt, nullFloat(&o.FilledPrice), &o.Fees,
		nullFloat(&o.SlippageBps), &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PaperOrder{}, ErrOrderNotFound
	}
	if err != nil {
		return domain.PaperOrder{}, fmt.Errorf("repository: scan paper order: %w", err)
	}

	o.TradeSignalID = tradeSignalID

	o.SubmittedAt, err = parseTime(submittedAt)
	if err != nil {
		return domain.PaperOrder{}, err
	}
	o.FilledAt, err = parseNullableTime(filledAt)
	if err != nil {
		return domain.PaperOrder{}, err
	}
	o.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.PaperOrder{}, err
	}
	return o, nil
}
