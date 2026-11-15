// Package jquantslabel is the part of the research label record that the J-Quants
// adapter (issue #1093) uses: it re-labels a JST day with minute bars from J-Quants
// where market_snapshots had none, and deletes those rows again when the
// subscription is cancelled.
//
// Rows it writes carry research_horizon_labels.exit_source = 'jquants' (migration
// 000058); every row the recorder of issue #1074 wrote is 'snapshot'. It writes
// only the research label tables (isolation_test.go in the parent package pins
// that) and reads market_snapshots only through the parent Repository and one
// symbol listing. It is never a trading input.
package jquantslabel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// SourceJQuants is research_horizon_labels.exit_source of a row whose exit bar
// came from J-Quants.
const SourceJQuants = "jquants"

// Repository adds the J-Quants re-labeling queries to the label Repository: it
// embeds it, so InstrumentIDs, Bars (the entries, with their bid and ask) and
// MarkDone (the day's marks and sums) are the recorder's own.
type Repository struct {
	*researchlabel.Repository
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{Repository: researchlabel.NewRepository(db), db: db}
}

// SnapshotSymbols are the 4-character symbols with a market_snapshots bar on the
// JST day: the issues whose minute bars are worth fetching for it.
func (r *Repository) SnapshotSymbols(ctx context.Context, day string) ([]string, error) {
	from, to, err := researchlabel.DayBounds(day)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM market_snapshots WHERE timestamp >= ? AND timestamp < ? ORDER BY symbol`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("jquantslabel: list symbols of %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("jquantslabel: scan symbol: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordedSnapshots is the number of bars the recorder counted when it marked the
// JST day done, and whether it has (a day without the mark is not re-labeled: the
// recorder will label it whole, with the minute bars that exist by then unused).
func (r *Repository) RecordedSnapshots(ctx context.Context, day string) (snapshots int, marked bool, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT snapshots FROM research_label_days WHERE day = ?`, day).Scan(&snapshots)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("jquantslabel: read the mark of %s: %w", day, err)
	}
	return snapshots, true, nil
}

const insertSQL = `INSERT OR IGNORE INTO research_horizon_labels (
	snapshot_id, instrument_id, symbol, snapshot_at, horizon, exit_at, entry_bid, entry_ask, entry_price, exit_price, spread_bps,
	long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version, created_at, day, sample_bucket, exit_source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'jquants')`

// jst is the zone of the day a label belongs to.
var jst = time.FixedZone("JST", 9*60*60)

// InsertJQuants adds the re-labeled rows in one transaction and returns how many
// were new: a (snapshot, horizon) the recorder already labeled is left exactly as
// it is (INSERT OR IGNORE), so a row that exists is never rewritten, and a
// re-run adds nothing.
func (r *Repository) InsertJQuants(ctx context.Context, rows []rlabel.Label, createdAt time.Time) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("jquantslabel: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("jquantslabel: prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	created := sqlutil.FormatTime(createdAt)
	inserted := 0
	for _, l := range rows {
		var long, short [3]any
		if l.Long != nil {
			long = [3]any{l.Long.GrossBps, l.Long.CostBps, l.Long.NetBps}
		}
		if l.Short != nil {
			short = [3]any{l.Short.GrossBps, l.Short.CostBps, l.Short.NetBps}
		}
		res, err := stmt.ExecContext(ctx, l.SnapshotID, l.InstrumentID, l.Symbol, sqlutil.FormatTime(l.SnapshotAt), l.Horizon,
			sqlutil.FormatTime(l.ExitAt), sqlutil.NullableFloat64(l.EntryBid), sqlutil.NullableFloat64(l.EntryAsk), l.EntryPrice, l.ExitPrice,
			sqlutil.NullableFloat64(l.SpreadBps), long[0], long[1], long[2], short[0], short[1], short[2], rlabel.LabelVersion, created,
			l.SnapshotAt.In(jst).Format(time.DateOnly), researchlabel.SampleBucket(l.SnapshotID))
		if err != nil {
			return 0, fmt.Errorf("jquantslabel: insert snapshot %d horizon %s: %w", l.SnapshotID, l.Horizon, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("jquantslabel: rows affected: %w", err)
		}
		inserted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("jquantslabel: commit: %w", err)
	}
	return inserted, nil
}

// DeleteJQuants deletes up to limit rows whose exit bar came from J-Quants and
// returns how many, with the JST days they belonged to (so the caller can refresh
// those days' sums). Rows with exit_source 'snapshot' are never touched. One
// short transaction; the rows are found through research_horizon_labels_jquants_idx.
func (r *Repository) DeleteJQuants(ctx context.Context, limit int) (deleted int, days []string, err error) {
	if limit <= 0 {
		return 0, nil, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("jquantslabel: begin purge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id, day FROM research_horizon_labels WHERE exit_source = 'jquants' ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return 0, nil, fmt.Errorf("jquantslabel: list rows to purge: %w", err)
	}
	var ids []any
	seen := map[string]bool{}
	for rows.Next() {
		var (
			id  int64
			day string
		)
		if err := rows.Scan(&id, &day); err != nil {
			_ = rows.Close()
			return 0, nil, fmt.Errorf("jquantslabel: scan row to purge: %w", err)
		}
		ids = append(ids, id)
		if !seen[day] {
			seen[day] = true
			days = append(days, day)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, nil, err
	}
	_ = rows.Close()
	if len(ids) == 0 {
		return 0, nil, nil
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM research_horizon_labels WHERE exit_source = 'jquants' AND id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, ids...) //nolint:gosec // G202: only "?" markers are concatenated; the ids are bound parameters
	if err != nil {
		return 0, nil, fmt.Errorf("jquantslabel: purge rows: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil, fmt.Errorf("jquantslabel: purged rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("jquantslabel: commit purge: %w", err)
	}
	return int(n), days, nil
}
