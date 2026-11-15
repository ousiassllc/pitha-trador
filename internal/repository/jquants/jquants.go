// Package jquants persists the J-Quants read adapter's research data (issue
// #1093, migration 000058): the listed-issue master, daily bars, minute bars, the
// per-(dataset, day, issue) coverage marks and the per-day request ledger.
//
// The data is research-only and private-use (J-Quants' terms). Nothing here is a
// trading input: Policy, Risk, order placement, the Paper guard, Live, the Jev
// prompts, RAG, calibration and the meta-model training never read it
// (isolation_test.go pins that). It never touches market_snapshots, daily_bars or
// disclosure_items. Every table carries source = 'jquants', and Purge deletes
// exactly these tables. It MUST depend only on internal/domain and
// internal/repository/sqlutil.
package jquants

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jquants"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Tables are the J-Quants tables the repository writes, in the order Purge
// empties them. research_jquants_fetch_log is deliberately not among them: it
// holds counts only (no market data), and deleting it would reset the day's
// request counter in the middle of the day.
var Tables = []string{
	"research_jquants_minute_bars",
	"research_jquants_daily_bars",
	"research_jquants_symbols",
	"research_jquants_coverage",
}

// Repository reads and writes the J-Quants research tables.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

const upsertSymbolSQL = `INSERT INTO research_jquants_symbols (as_of, code, symbol, company_name, company_name_en, sector17_code, sector33_code,
	scale_category, market_code, market_name, fetched_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (as_of, code) DO UPDATE SET symbol = excluded.symbol, company_name = excluded.company_name, company_name_en = excluded.company_name_en,
	sector17_code = excluded.sector17_code, sector33_code = excluded.sector33_code, scale_category = excluded.scale_category,
	market_code = excluded.market_code, market_name = excluded.market_name, fetched_at = excluded.fetched_at`

// UpsertSymbols writes the master rows in one transaction (a re-fetch of the same
// (as_of, code) overwrites the row) and returns how many it wrote.
func (r *Repository) UpsertSymbols(ctx context.Context, rows []jquants.Symbol, fetchedAt time.Time) (int, error) {
	at := sqlutil.FormatTime(fetchedAt)
	return r.upsert(ctx, "symbols", upsertSymbolSQL, len(rows), func(i int) []any {
		s := rows[i]
		return []any{s.AsOf, s.Code, s.Symbol, s.CompanyName, s.CompanyNameEn, s.Sector17Code, s.Sector33Code, s.ScaleCategory, s.MarketCode, s.MarketName, at}
	})
}

const upsertDailySQL = `INSERT INTO research_jquants_daily_bars (day, code, symbol, open, high, low, close, volume, turnover, adj_factor,
	adj_open, adj_high, adj_low, adj_close, adj_volume, upper_limit, lower_limit, fetched_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (day, code) DO UPDATE SET symbol = excluded.symbol, open = excluded.open, high = excluded.high, low = excluded.low, close = excluded.close,
	volume = excluded.volume, turnover = excluded.turnover, adj_factor = excluded.adj_factor, adj_open = excluded.adj_open, adj_high = excluded.adj_high,
	adj_low = excluded.adj_low, adj_close = excluded.adj_close, adj_volume = excluded.adj_volume, upper_limit = excluded.upper_limit,
	lower_limit = excluded.lower_limit, fetched_at = excluded.fetched_at`

// UpsertDailyBars writes the daily bars in one transaction (a re-fetch of the
// same (day, code) overwrites: J-Quants corrects data in place) and returns how
// many it wrote.
func (r *Repository) UpsertDailyBars(ctx context.Context, rows []jquants.DailyBar, fetchedAt time.Time) (int, error) {
	at := sqlutil.FormatTime(fetchedAt)
	return r.upsert(ctx, "daily bars", upsertDailySQL, len(rows), func(i int) []any {
		b := rows[i]
		return []any{b.Day, b.Code, b.Symbol, sqlutil.NullableFloat64(b.Open), sqlutil.NullableFloat64(b.High), sqlutil.NullableFloat64(b.Low),
			sqlutil.NullableFloat64(b.Close), sqlutil.NullableFloat64(b.Volume), sqlutil.NullableFloat64(b.Turnover), sqlutil.NullableFloat64(b.AdjFactor),
			sqlutil.NullableFloat64(b.AdjOpen), sqlutil.NullableFloat64(b.AdjHigh), sqlutil.NullableFloat64(b.AdjLow), sqlutil.NullableFloat64(b.AdjClose),
			sqlutil.NullableFloat64(b.AdjVolume), b.UpperLimit, b.LowerLimit, at}
	})
}

const upsertMinuteSQL = `INSERT INTO research_jquants_minute_bars (code, symbol, day, bar_at, open, high, low, close, volume, turnover, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (code, bar_at) DO UPDATE SET symbol = excluded.symbol, day = excluded.day, open = excluded.open, high = excluded.high, low = excluded.low,
	close = excluded.close, volume = excluded.volume, turnover = excluded.turnover, fetched_at = excluded.fetched_at`

// UpsertMinuteBars writes the minute bars in one transaction (idempotent on
// (code, bar_at)) and returns how many it wrote.
func (r *Repository) UpsertMinuteBars(ctx context.Context, rows []jquants.MinuteBar, fetchedAt time.Time) (int, error) {
	at := sqlutil.FormatTime(fetchedAt)
	return r.upsert(ctx, "minute bars", upsertMinuteSQL, len(rows), func(i int) []any {
		b := rows[i]
		return []any{b.Code, b.Symbol, b.Day, sqlutil.FormatTime(b.At), b.Open, b.High, b.Low, b.Close, b.Volume, b.Turnover, at}
	})
}

// upsert runs stmt n times with args(i) in one short transaction.
func (r *Repository) upsert(ctx context.Context, what, stmt string, n int, args func(i int) []any) (int, error) {
	if n == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("jquants: begin %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()
	prepared, err := tx.PrepareContext(ctx, stmt)
	if err != nil {
		return 0, fmt.Errorf("jquants: prepare %s: %w", what, err)
	}
	defer func() { _ = prepared.Close() }()
	for i := range n {
		if _, err := prepared.ExecContext(ctx, args(i)...); err != nil {
			return 0, fmt.Errorf("jquants: write %s: %w", what, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("jquants: commit %s: %w", what, err)
	}
	return n, nil
}
