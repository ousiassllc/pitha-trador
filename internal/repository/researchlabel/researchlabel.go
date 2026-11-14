// Package researchlabel persists the research-only fixed-horizon labels
// (issue #1074): research_horizon_labels, one row per (market snapshot,
// horizon: 30m / 60m / close), research_label_days, the per-JST-day mark that
// a day is fully recorded, and research_label_day_stats, the day's sums the
// MCP tool aggregates from (migration 000055).
//
// The record is for research only. Policy, Risk, order placement, the Paper
// guard, Live, the Jev prompts, RAG, the meta-model training and
// self-improvement never read it: the only users are the recorder
// (internal/service/researchlabel, wired by internal/bootstrap/researchlabeljob)
// and the read-only MCP tool research_horizon_summary. isolation_test.go pins
// that. It only reads market_snapshots and writes only its own three tables; it
// MUST depend only on internal/domain and internal/repository/sqlutil. See
// docs/architecture/overview.md §3.
package researchlabel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// jst is the zone of the days research labels belong to. Japan has no DST, so
// a fixed zone is exact.
var jst = time.FixedZone("JST", 9*60*60)

// Repository reads market_snapshots bars and writes research label rows.
type Repository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by db.
func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// DayBounds is the half-open UTC range [from, to) of the JST day ("2006-01-02").
func DayBounds(day string) (from, to time.Time, err error) {
	d, err := time.ParseInLocation(time.DateOnly, day, jst)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("researchlabel: day %q: %w", day, err)
	}
	return d, d.AddDate(0, 0, 1), nil
}

// PendingDays returns up to limit JST days strictly before the day `before`,
// oldest first, that have at least one market_snapshots bar and no
// research_label_days mark. A day whose snapshots retention has purged is not
// listed (nothing is left to label). It reads the oldest snapshot time and one
// existence probe per unmarked day, all served by market_snapshots_timestamp_idx.
func (r *Repository) PendingDays(ctx context.Context, before string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	var oldest sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MIN(timestamp) FROM market_snapshots`).Scan(&oldest); err != nil {
		return nil, fmt.Errorf("researchlabel: oldest snapshot: %w", err)
	}
	if !oldest.Valid {
		return nil, nil
	}
	first, err := sqlutil.ParseTime(oldest.String)
	if err != nil {
		return nil, fmt.Errorf("researchlabel: oldest snapshot: %w", err)
	}
	start := first.In(jst).Format(time.DateOnly)
	marked, err := r.markedDays(ctx, start)
	if err != nil {
		return nil, err
	}
	var days []string
	day, err := time.ParseInLocation(time.DateOnly, start, jst)
	if err != nil {
		return nil, err
	}
	for ; len(days) < limit; day = day.AddDate(0, 0, 1) {
		name := day.Format(time.DateOnly)
		if name >= before {
			break
		}
		if marked[name] {
			continue
		}
		var one int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM market_snapshots WHERE timestamp >= ? AND timestamp < ? LIMIT 1`,
			sqlutil.FormatTime(day), sqlutil.FormatTime(day.AddDate(0, 0, 1))).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("researchlabel: probe snapshots of %s: %w", name, err)
		}
		days = append(days, name)
	}
	return days, nil
}

func (r *Repository) markedDays(ctx context.Context, from string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT day FROM research_label_days WHERE day >= ?`, from)
	if err != nil {
		return nil, fmt.Errorf("researchlabel: list recorded days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	marked := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("researchlabel: scan recorded day: %w", err)
		}
		marked[day] = true
	}
	return marked, rows.Err()
}

// InstrumentIDs returns the instruments with a bar on the JST day.
func (r *Repository) InstrumentIDs(ctx context.Context, day string) ([]int64, error) {
	from, to, err := DayBounds(day)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT instrument_id FROM market_snapshots WHERE timestamp >= ? AND timestamp < ? ORDER BY instrument_id`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("researchlabel: list instruments of %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("researchlabel: scan instrument: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Bars returns the bars of one instrument on the JST day, ascending, with only
// the columns the label needs (no raw board).
func (r *Repository) Bars(ctx context.Context, day string, instrumentID int64) ([]domain.Snapshot, error) {
	from, to, err := DayBounds(day)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, instrument_id, symbol, timestamp, price, bid, ask, spread_bps
FROM market_snapshots WHERE instrument_id = ? AND timestamp >= ? AND timestamp < ? ORDER BY timestamp ASC`,
		instrumentID, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("researchlabel: list bars of instrument %d on %s: %w", instrumentID, day, err)
	}
	defer func() { _ = rows.Close() }()
	var bars []domain.Snapshot
	for rows.Next() {
		var (
			s  domain.Snapshot
			ts string
		)
		if err := rows.Scan(&s.ID, &s.InstrumentID, &s.Symbol, &ts, &s.Price,
			sqlutil.NullFloat(&s.Bid), sqlutil.NullFloat(&s.Ask), sqlutil.NullFloat(&s.SpreadBps)); err != nil {
			return nil, fmt.Errorf("researchlabel: scan bar: %w", err)
		}
		if s.Timestamp, err = sqlutil.ParseTime(ts); err != nil {
			return nil, fmt.Errorf("researchlabel: bar %d timestamp: %w", s.ID, err)
		}
		bars = append(bars, s)
	}
	return bars, rows.Err()
}

const insertSQL = `INSERT OR IGNORE INTO research_horizon_labels (
	snapshot_id, instrument_id, symbol, snapshot_at, horizon, exit_at, entry_bid, entry_ask, entry_price, exit_price, spread_bps,
	long_gross_bps, long_cost_bps, long_net_bps, short_gross_bps, short_cost_bps, short_net_bps, label_version, created_at, day, sample_bucket
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// SampleBuckets is the number of research_horizon_labels.sample_bucket values.
const SampleBuckets = 100

// SampleBucket is the sample_bucket of a snapshot: a multiplicative hash of its
// id modulo SampleBuckets. Consecutive ids of one instrument are a cycle's
// instrument count apart, so a plain modulo would pick the same instruments
// every time; the hash spreads instruments and minutes evenly, so reading the
// buckets below k is a k% sample of the rows.
func SampleBucket(snapshotID int64) int {
	const golden = 0x9E3779B97F4A7C15
	return int(((uint64(snapshotID) * golden) >> 32) % SampleBuckets) //nolint:gosec // G115: a hash, the bits are what is mixed in
}

// Insert writes rows in one transaction and returns how many were new: a row
// whose (snapshot_id, horizon) is already recorded is left untouched (INSERT OR
// IGNORE), which makes a re-run idempotent. Callers keep batches small (one
// instrument-day) so the write lock is held briefly.
func (r *Repository) Insert(ctx context.Context, rows []rlabel.Label, createdAt time.Time) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("researchlabel: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("researchlabel: prepare insert: %w", err)
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
			l.SnapshotAt.In(jst).Format(time.DateOnly), SampleBucket(l.SnapshotID))
		if err != nil {
			return 0, fmt.Errorf("researchlabel: insert snapshot %d horizon %s: %w", l.SnapshotID, l.Horizon, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("researchlabel: rows affected: %w", err)
		}
		inserted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("researchlabel: commit: %w", err)
	}
	return inserted, nil
}
