package trading

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// PositionRepository persists positions rows: held (or closed) Paper/Live
// positions, Entry/Exit order-linked (docs/architecture/er.md
// §positions, functional.md §4.9 状態管理's per-symbol "position" field).
type PositionRepository struct {
	db *sql.DB
}

// NewPositionRepository returns a PositionRepository backed by db.
func NewPositionRepository(db *sql.DB) *PositionRepository {
	return &PositionRepository{db: db}
}

const insertPositionSQL = `
INSERT INTO positions (
	instrument_id, entry_order_id, exit_order_id, symbol, side, quantity,
	entry_price, current_price, unrealized_pnl, realized_pnl, opened_at,
	closed_at, exit_reason, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Open writes a new positions row (Paper Entry, functional.md
// FR-ENTRY-1). p.ExitOrderID/ClosedAt/ExitReason/RealizedPnL must be nil;
// positions_open_instrument_uq rejects opening a second position for the
// same instrument while one is already open.
func (r *PositionRepository) Open(ctx context.Context, p domain.Position) (domain.Position, error) {
	return openPosition(ctx, r.db, p)
}

// openPosition is Open's body over any sqlutil.Executor, so
// OrderRepository.FillEntry can run it inside its own transaction.
func openPosition(ctx context.Context, x sqlutil.Executor, p domain.Position) (domain.Position, error) {
	now := p.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	res, err := x.ExecContext(ctx, insertPositionSQL,
		p.InstrumentID, p.EntryOrderID, sqlutil.NullableInt64(p.ExitOrderID), p.Symbol, p.Side, p.Quantity,
		p.EntryPrice, p.CurrentPrice, p.UnrealizedPnL, sqlutil.NullableFloat64(p.RealizedPnL), sqlutil.FormatTime(p.OpenedAt),
		sqlutil.NullableTime(p.ClosedAt), sqlutil.NullableString(p.ExitReason), sqlutil.FormatTime(now), sqlutil.FormatTime(now),
	)
	if err != nil {
		return domain.Position{}, fmt.Errorf(
			"repository: open position for instrument %d (%s): %w", p.InstrumentID, p.Side, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.Position{}, fmt.Errorf("repository: read position id for instrument %d: %w", p.InstrumentID, err)
	}

	p.ID = id
	p.CreatedAt = now
	p.UpdatedAt = now
	return p, nil
}

// Get returns the positions row with the given id, or
// ErrPositionNotFound.
func (r *PositionRepository) Get(ctx context.Context, id int64) (domain.Position, error) {
	row := r.db.QueryRowContext(ctx, positionSelectColumns+` WHERE id = ?`, id)
	return scanPosition(row)
}

// GetOpenByInstrument returns instrumentID's currently open position
// (ClosedAt == nil), or ErrPositionNotFound if it has none
// (positions_open_instrument_uq guarantees at most one such row).
func (r *PositionRepository) GetOpenByInstrument(ctx context.Context, instrumentID int64) (domain.Position, error) {
	row := r.db.QueryRowContext(ctx,
		positionSelectColumns+` WHERE instrument_id = ? AND closed_at IS NULL`, instrumentID)
	return scanPosition(row)
}

// Mark updates an open position's current_price/unrealized_pnl (mark to
// market, functional.md §4.10 FR-SCHED-4's 5-15秒周期の保有ポジション再評
// 価). It returns ErrPositionNotFound if id does not exist.
func (r *PositionRepository) Mark(ctx context.Context, id int64, currentPrice, unrealizedPnL float64, now time.Time) (domain.Position, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE positions SET current_price = ?, unrealized_pnl = ?, updated_at = ? WHERE id = ? AND closed_at IS NULL`,
		currentPrice, unrealizedPnL, sqlutil.FormatTime(now), id,
	)
	if err != nil {
		return domain.Position{}, fmt.Errorf("repository: mark position %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.Position{}, domain.ErrPositionNotFound
	}
	return r.Get(ctx, id)
}

// Close closes an open position (Paper Exit, functional.md FR-EXIT-1):
// links exitOrderID, sets closedAt/exitReason/realizedPnL, and marks the
// final current_price. It returns ErrPositionNotFound if id does not
// exist or is already closed.
func (r *PositionRepository) Close(ctx context.Context, id, exitOrderID int64, exitPrice, realizedPnL float64, exitReason string, now time.Time) (domain.Position, error) {
	return closePosition(ctx, r.db, id, exitOrderID, exitPrice, realizedPnL, exitReason, now)
}

// closePosition is Close's body over any sqlutil.Executor, so
// OrderRepository.FillExit can run it inside its own transaction.
func closePosition(ctx context.Context, x sqlutil.Executor, id, exitOrderID int64, exitPrice, realizedPnL float64, exitReason string, now time.Time) (domain.Position, error) {
	res, err := x.ExecContext(ctx,
		`UPDATE positions
		 SET exit_order_id = ?, current_price = ?, realized_pnl = ?, closed_at = ?, exit_reason = ?, updated_at = ?
		 WHERE id = ? AND closed_at IS NULL`,
		exitOrderID, exitPrice, realizedPnL, sqlutil.FormatTime(now), exitReason, sqlutil.FormatTime(now), id,
	)
	if err != nil {
		return domain.Position{}, fmt.Errorf("repository: close position %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.Position{}, domain.ErrPositionNotFound
	}
	return scanPosition(x.QueryRowContext(ctx, positionSelectColumns+` WHERE id = ?`, id))
}

// CloseWithExitOrder submits exitOrder (PENDING), fills it at price/now and
// closes positionID with it in ONE transaction: afterwards either the FILLED
// exit order and the closed position both exist, or neither. A concurrent
// close of the same position matches no open row (domain.ErrPositionNotFound)
// and rolls its order insert back, so no orphan FILLED exit order is left for
// risk.Engine's reconciliation to report as a fill discrepancy (issue #174).
func (r *PositionRepository) CloseWithExitOrder(ctx context.Context, exitOrder domain.PaperOrder, price float64, positionID int64, realizedPnL float64, exitReason string, now time.Time) (domain.Position, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Position{}, fmt.Errorf("repository: begin close transaction for position %d: %w", positionID, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	inserted, err := insertOrder(ctx, tx, exitOrder)
	if err != nil {
		return domain.Position{}, err
	}
	filled, err := fillOrder(ctx, tx, inserted.ID, price, nil, now)
	if err != nil {
		return domain.Position{}, err
	}
	closed, err := closePosition(ctx, tx, positionID, filled.ID, price, realizedPnL, exitReason, now)
	if err != nil {
		return domain.Position{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Position{}, fmt.Errorf("repository: commit close transaction for position %d: %w", positionID, err)
	}
	return closed, nil
}

// ListOpen returns every currently open position (ClosedAt == nil),
// oldest first, for FR-RISK-1's open-position-count/exposure checks and
// FR-SCHED-4's held-position Exit re-evaluation cycle.
func (r *PositionRepository) ListOpen(ctx context.Context) ([]domain.Position, error) {
	rows, err := r.db.QueryContext(ctx, positionSelectColumns+` WHERE closed_at IS NULL ORDER BY opened_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("repository: list open positions: %w", err)
	}
	return scanPositions(rows)
}

// ListClosedBetween returns every position closed in [start, end),
// oldest first, for internal/service/selfimprove.Governor's
// FR-SELFIMPROVE-6 post-apply realized-Expectancy tracking (comparing
// the mean realized_pnl of positions closed in the 5-business-day window
// before/after a proposal's applied_at).
func (r *PositionRepository) ListClosedBetween(ctx context.Context, start, end time.Time) ([]domain.Position, error) {
	rows, err := r.db.QueryContext(ctx,
		positionSelectColumns+` WHERE closed_at IS NOT NULL AND closed_at >= ? AND closed_at < ? ORDER BY closed_at ASC`,
		sqlutil.FormatTime(start), sqlutil.FormatTime(end),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list closed positions between %s and %s: %w", start, end, err)
	}
	return scanPositions(rows)
}

// List returns up to limit positions rows (open and closed), most
// recently opened first, for `GET /api/v1/positions`
// (docs/api/endpoints.md §5: "現在保有中および直近クローズ済みポジショ
// ン一覧").
func (r *PositionRepository) List(ctx context.Context, limit int) ([]domain.Position, error) {
	rows, err := r.db.QueryContext(ctx, positionSelectColumns+` ORDER BY opened_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list positions: %w", err)
	}
	return scanPositions(rows)
}

func scanPositions(rows *sql.Rows) ([]domain.Position, error) {
	defer func() { _ = rows.Close() }()

	var out []domain.Position
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list positions: %w", err)
	}
	return out, nil
}

const positionSelectColumns = `
SELECT id, instrument_id, entry_order_id, exit_order_id, symbol, side, quantity,
	entry_price, current_price, unrealized_pnl, realized_pnl, opened_at,
	closed_at, exit_reason, created_at, updated_at
FROM positions`

func scanPosition(row sqlutil.RowScanner) (domain.Position, error) {
	var (
		p           domain.Position
		exitOrderID *int64
		openedAt    string
		closedAt    sql.NullString
		exitReason  sql.NullString
		createdAt   string
		updatedAt   string
	)

	err := row.Scan(
		&p.ID, &p.InstrumentID, &p.EntryOrderID, sqlutil.NullInt64(&exitOrderID), &p.Symbol, &p.Side, &p.Quantity,
		&p.EntryPrice, &p.CurrentPrice, &p.UnrealizedPnL, sqlutil.NullFloat(&p.RealizedPnL), &openedAt,
		&closedAt, &exitReason, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Position{}, domain.ErrPositionNotFound
	}
	if err != nil {
		return domain.Position{}, fmt.Errorf("repository: scan position: %w", err)
	}

	p.ExitOrderID = exitOrderID
	if exitReason.Valid {
		p.ExitReason = &exitReason.String
	}

	p.OpenedAt, err = sqlutil.ParseTime(openedAt)
	if err != nil {
		return domain.Position{}, err
	}
	p.ClosedAt, err = sqlutil.ParseNullableTime(closedAt)
	if err != nil {
		return domain.Position{}, err
	}
	p.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return domain.Position{}, err
	}
	p.UpdatedAt, err = sqlutil.ParseTime(updatedAt)
	if err != nil {
		return domain.Position{}, err
	}
	return p, nil
}
