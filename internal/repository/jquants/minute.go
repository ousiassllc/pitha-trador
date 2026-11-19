package jquants

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// MinuteBars returns the stored minute bars of the 4-character symbol on the JST
// day, ascending by time.
func (r *Repository) MinuteBars(ctx context.Context, day, symbol string) ([]jquants.MinuteBar, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code, symbol, day, bar_at, open, high, low, close, volume, turnover
FROM research_jquants_minute_bars WHERE day = ? AND symbol = ? ORDER BY bar_at ASC`, day, symbol)
	if err != nil {
		return nil, fmt.Errorf("jquants: list minute bars of %s on %s: %w", symbol, day, err)
	}
	defer func() { _ = rows.Close() }()
	var bars []jquants.MinuteBar
	for rows.Next() {
		var (
			b  jquants.MinuteBar
			at string
		)
		if err := rows.Scan(&b.Code, &b.Symbol, &b.Day, &at, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.Turnover); err != nil {
			return nil, fmt.Errorf("jquants: scan minute bar: %w", err)
		}
		if b.At, err = sqlutil.ParseTime(at); err != nil {
			return nil, fmt.Errorf("jquants: minute bar time: %w", err)
		}
		bars = append(bars, b)
	}
	return bars, rows.Err()
}

// PurgeBatch deletes up to limit rows from each of Tables and returns how many it
// deleted per table; done is true when every table is empty afterwards. Only
// these tables are touched, and every row of them is J-Quants data (source
// 'jquants' is the only value the schema allows), so what is left in any other
// table is not. One short transaction per table, so a large store is removed in
// bounded steps (the caller repeats until done).
func (r *Repository) PurgeBatch(ctx context.Context, limit int) (deleted map[string]int, done bool, err error) {
	deleted = map[string]int{}
	if limit <= 0 {
		return deleted, false, nil
	}
	done = true
	for _, table := range Tables {
		res, err := r.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid IN (SELECT rowid FROM `+table+` LIMIT ?)`, limit) //nolint:gosec // G202: table names are the package's own constants
		if err != nil {
			return deleted, false, fmt.Errorf("jquants: purge %s: %w", table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return deleted, false, fmt.Errorf("jquants: purged rows of %s: %w", table, err)
		}
		deleted[table] = int(n)
		if int(n) == limit {
			done = false
		}
	}
	return deleted, done, nil
}
