// Package researchindex persists the research-only index record (issue #1084):
// research_index_minutes, one row per (index code, session minute) with the value
// 立花 quoted, and research_index_days, the per-trading-day marker that the day
// was checked (with values or, when 立花 had none, as unavailable) (migration
// 000069).
//
// The values are read from research_quote_samples, the 立花 quote recording of
// issue #1083 (a SELECT: that table is never written here), so the record makes
// no request to any broker or service. The record is for research only. Feature
// Engine, Policy, Risk, order placement, execution, the Paper guard, Live, the
// Jev prompts, RAG, the meta-model training and self-improvement never read it:
// the only users are the recorder (internal/service/researchindex, wired by
// internal/bootstrap/researchindexjob) and the read-only MCP tools
// (paperresultsmcp/indexdiag). isolation_test.go pins that. It writes only its
// own two tables and MUST depend only on internal/domain and
// internal/repository/sqlutil: no broker, kabu or tachibana client.
package researchindex

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	rindex "github.com/ousiassllc/pitha-trador/internal/domain/researchindex"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// sourceTachibana is research_quote_samples.source of the 立花 recording.
const sourceTachibana = "tachibana"

// Repository reads the 立花 quote samples and writes the index record.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Samples returns the 立花 values of symbol stamped on the JST date day, oldest
// first, as research_quote_samples holds them (one per minute: the table's key is
// (source, symbol, timestamp)). It only reads.
func (r *Repository) Samples(ctx context.Context, symbol, day string) ([]rindex.Minute, error) {
	start, err := rindex.DayStart(day)
	if err != nil {
		return nil, fmt.Errorf("researchindex: trading day %q is not YYYY-MM-DD: %w", day, err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT timestamp, price, via FROM research_quote_samples
WHERE source = ? AND symbol = ? AND timestamp >= ? AND timestamp < ? ORDER BY timestamp`,
		sourceTachibana, symbol, sqlutil.FormatTime(start), sqlutil.FormatTime(start.AddDate(0, 0, 1)))
	if err != nil {
		return nil, fmt.Errorf("researchindex: read samples of %s %s: %w", symbol, day, err)
	}
	defer func() { _ = rows.Close() }()
	var out []rindex.Minute
	for rows.Next() {
		var ts, via string
		m := rindex.Minute{Symbol: symbol, TradingDay: day}
		if err := rows.Scan(&ts, &m.Price, &via); err != nil {
			return nil, fmt.Errorf("researchindex: scan sample of %s: %w", symbol, err)
		}
		if m.Time, err = sqlutil.ParseTime(ts); err != nil {
			return nil, err
		}
		m.Via = via
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkedDays returns the trading days of symbol in [from, to] (YYYY-MM-DD,
// inclusive) that have a marker.
func (r *Repository) MarkedDays(ctx context.Context, symbol, from, to string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT trading_day FROM research_index_days WHERE symbol = ? AND trading_day >= ? AND trading_day <= ?`, symbol, from, to)
	if err != nil {
		return nil, fmt.Errorf("researchindex: list marked days of %s: %w", symbol, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("researchindex: scan marked day: %w", err)
		}
		out[day] = true
	}
	return out, rows.Err()
}

// RecordDay writes the minutes of one (symbol, trading day) and the day's marker
// in one transaction: the marker is 'recorded' when minutes is not empty and
// 'unavailable' when it is. Minutes already stored are left as they are (ON
// CONFLICT DO NOTHING; unlike INSERT OR IGNORE it still fails on a CHECK
// violation) and the marker is replaced, so a rerun never duplicates a row and a
// day is never marked without its minutes. minute_count is the number of the day's
// rows after the write. It returns the marker written.
func (r *Repository) RecordDay(ctx context.Context, symbol, day string, minutes []rindex.Minute, at time.Time) (rindex.Day, error) {
	if _, err := rindex.DayStart(day); err != nil {
		return rindex.Day{}, fmt.Errorf("researchindex: trading day %q is not YYYY-MM-DD: %w", day, err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return rindex.Day{}, fmt.Errorf("researchindex: begin %s %s: %w", symbol, day, err)
	}
	defer func() { _ = tx.Rollback() }()
	recordedAt := sqlutil.FormatTime(at)
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO research_index_minutes (symbol, minute_time, trading_day, price, via, recorded_at)
	VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (symbol, minute_time) DO NOTHING`)
	if err != nil {
		return rindex.Day{}, fmt.Errorf("researchindex: prepare minute insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, m := range minutes {
		if m.Symbol != symbol || m.TradingDay != day {
			return rindex.Day{}, fmt.Errorf("researchindex: minute %s %s does not belong to %s %s", m.Symbol, m.TradingDay, symbol, day)
		}
		if _, err := stmt.ExecContext(ctx, m.Symbol, sqlutil.FormatTime(m.Time), m.TradingDay, m.Price, m.Via, recordedAt); err != nil {
			return rindex.Day{}, fmt.Errorf("researchindex: write minute %s %s: %w", m.Symbol, sqlutil.FormatTime(m.Time), err)
		}
	}
	d := rindex.Day{Symbol: symbol, TradingDay: day, Status: rindex.StatusUnavailable}
	if len(minutes) > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_index_minutes WHERE symbol = ? AND trading_day = ?`, symbol, day).Scan(&d.MinuteCount); err != nil {
			return rindex.Day{}, fmt.Errorf("researchindex: count %s %s: %w", symbol, day, err)
		}
		d.Status = rindex.StatusRecorded
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO research_index_days (symbol, trading_day, status, minute_count, checked_at) VALUES (?, ?, ?, ?, ?)`,
		symbol, day, d.Status, d.MinuteCount, recordedAt); err != nil {
		return rindex.Day{}, fmt.Errorf("researchindex: mark %s %s: %w", symbol, day, err)
	}
	if err := tx.Commit(); err != nil {
		return rindex.Day{}, fmt.Errorf("researchindex: commit %s %s: %w", symbol, day, err)
	}
	return d, nil
}
