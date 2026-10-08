package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// DailyBarRepository persists daily_bars: one row per symbol and 立会日, the
// local copy of the broker's daily history (docs/architecture/er/
// tables-market.md §daily_bars).
type DailyBarRepository struct {
	db *sql.DB
}

// NewDailyBarRepository returns a DailyBarRepository backed by db.
func NewDailyBarRepository(db *sql.DB) *DailyBarRepository {
	return &DailyBarRepository{db: db}
}

const dailyBarColumns = `symbol, trade_date, open, high, low, close, volume,
	adj_open, adj_high, adj_low, adj_close, adj_volume`

func scanDailyBar(row interface{ Scan(...any) error }) (domain.DailyBar, error) {
	var b domain.DailyBar
	err := row.Scan(&b.Symbol, &b.TradeDate, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume,
		&b.AdjOpen, &b.AdjHigh, &b.AdjLow, &b.AdjClose, &b.AdjVolume)
	return b, err
}

// Latest returns symbol's most recent stored bar, or nil when it has none.
func (r *DailyBarRepository) Latest(ctx context.Context, symbol string) (*domain.DailyBar, error) {
	b, err := scanDailyBar(r.db.QueryRowContext(ctx,
		`SELECT `+dailyBarColumns+` FROM daily_bars WHERE symbol = ? ORDER BY trade_date DESC LIMIT 1`, symbol))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repository: latest daily bar of %q: %w", symbol, err)
	}
	return &b, nil
}

// ListBySymbol returns every stored bar of symbol, oldest first.
func (r *DailyBarRepository) ListBySymbol(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+dailyBarColumns+` FROM daily_bars WHERE symbol = ? ORDER BY trade_date`, symbol)
	if err != nil {
		return nil, fmt.Errorf("repository: list daily bars of %q: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.DailyBar
	for rows.Next() {
		b, err := scanDailyBar(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan daily bar of %q: %w", symbol, err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list daily bars of %q: %w", symbol, err)
	}
	return out, nil
}

// Save stores bars of symbol in one transaction and returns how many rows it
// wrote. replace first deletes every stored bar of the symbol (a split
// changed the adjusted values of the whole history). A bar already stored for
// the same 立会日 is overwritten, so saving twice is harmless.
func (r *DailyBarRepository) Save(ctx context.Context, symbol string, bars []domain.DailyBar, replace bool) (int, error) {
	if len(bars) == 0 && !replace {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("repository: begin daily bar save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM daily_bars WHERE symbol = ?`, symbol); err != nil {
			return 0, fmt.Errorf("repository: replace daily bars of %q: %w", symbol, err)
		}
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO daily_bars (`+dailyBarColumns+`, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("repository: prepare daily bar save: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := sqlutil.FormatTime(time.Now())
	for _, b := range bars {
		if _, err := stmt.ExecContext(ctx, symbol, b.TradeDate, b.Open, b.High, b.Low, b.Close, b.Volume,
			b.AdjOpen, b.AdjHigh, b.AdjLow, b.AdjClose, b.AdjVolume, now); err != nil {
			return 0, fmt.Errorf("repository: save daily bar of %q: %w", symbol, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("repository: commit daily bar save: %w", err)
	}
	return len(bars), nil
}
