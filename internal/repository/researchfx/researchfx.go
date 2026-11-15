// Package researchfx persists the research-only FX 1-minute candle record
// (issue #1084): research_fx_candles, one row per (pair, minute) with GMO's BID
// and ASK candles merged, and research_fx_days, the per-trading-day marker that
// the day was fetched (with candles or, for a closed market, without) (migration
// 000058).
//
// The record is for research only. Feature Engine, Policy, Risk, order placement,
// execution, the Paper guard, Live, the Jev prompts, RAG, the meta-model training
// and self-improvement never read it: the only users are the recorder
// (internal/service/researchfx, wired by internal/bootstrap/researchfxjob) and the
// read-only MCP tools (paperresultsmcp/fxdiag). isolation_test.go pins that. It
// writes only its own two tables and MUST depend only on internal/domain and
// internal/repository/sqlutil: no broker, kabu or tachibana client.
package researchfx

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	rfx "github.com/ousiassllc/pitha-trador/internal/domain/researchfx"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository writes the FX candle record.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// MarkedDays returns the trading days of symbol in [from, to] (YYYY-MM-DD,
// inclusive) that have a marker.
func (r *Repository) MarkedDays(ctx context.Context, symbol, from, to string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT trading_day FROM research_fx_days WHERE symbol = ? AND trading_day >= ? AND trading_day <= ?`, symbol, from, to)
	if err != nil {
		return nil, fmt.Errorf("researchfx: list marked days of %s: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("researchfx: scan marked day: %w", err)
		}
		out[day] = true
	}
	return out, rows.Err()
}

// RecordDay writes the merged candles of one (symbol, trading day) and the day's
// marker in one transaction: the marker is 'recorded' when candles is not empty
// and 'empty' (a closed market) when it is. Candles already stored are left as
// they are (ON CONFLICT DO NOTHING; unlike INSERT OR IGNORE it still fails on a
// CHECK violation) and the marker is replaced, so a rerun never duplicates a row
// and a day is never marked without its candles. candle_count is
// the number of the day's rows after the write. It returns the marker written.
func (r *Repository) RecordDay(ctx context.Context, symbol, day string, candles []rfx.Candle, at time.Time) (rfx.Day, error) {
	if _, err := rfx.DayStart(day); err != nil {
		return rfx.Day{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return rfx.Day{}, fmt.Errorf("researchfx: begin %s %s: %w", symbol, day, err)
	}
	defer func() { _ = tx.Rollback() }()
	recordedAt := sqlutil.FormatTime(at)
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO research_fx_candles (symbol, open_time, trading_day,
	bid_open, bid_high, bid_low, bid_close, ask_open, ask_high, ask_low, ask_close, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (symbol, open_time) DO NOTHING`)
	if err != nil {
		return rfx.Day{}, fmt.Errorf("researchfx: prepare candle insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, c := range candles {
		if c.Symbol != symbol || c.TradingDay != day {
			return rfx.Day{}, fmt.Errorf("researchfx: candle %s %s does not belong to %s %s", c.Symbol, c.TradingDay, symbol, day)
		}
		args := []any{c.Symbol, sqlutil.FormatTime(c.OpenTime), c.TradingDay}
		for _, side := range []*rfx.OHLC{c.Bid, c.Ask} {
			if side == nil {
				args = append(args, nil, nil, nil, nil)
				continue
			}
			args = append(args, side.Open, side.High, side.Low, side.Close)
		}
		if _, err := stmt.ExecContext(ctx, append(args, recordedAt)...); err != nil {
			return rfx.Day{}, fmt.Errorf("researchfx: write candle %s %s: %w", c.Symbol, sqlutil.FormatTime(c.OpenTime), err)
		}
	}
	d := rfx.Day{Symbol: symbol, TradingDay: day, Status: rfx.StatusEmpty}
	if len(candles) > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_fx_candles WHERE symbol = ? AND trading_day = ?`, symbol, day).Scan(&d.CandleCount); err != nil {
			return rfx.Day{}, fmt.Errorf("researchfx: count %s %s: %w", symbol, day, err)
		}
		d.Status = rfx.StatusRecorded
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO research_fx_days (symbol, trading_day, status, candle_count, fetched_at) VALUES (?, ?, ?, ?, ?)`,
		symbol, day, d.Status, d.CandleCount, recordedAt); err != nil {
		return rfx.Day{}, fmt.Errorf("researchfx: mark %s %s: %w", symbol, day, err)
	}
	if err := tx.Commit(); err != nil {
		return rfx.Day{}, fmt.Errorf("researchfx: commit %s %s: %w", symbol, day, err)
	}
	return d, nil
}
