package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// ErrSnapshotNotFound is returned by SnapshotRepository methods when no
// matching market_snapshots row exists.
var ErrSnapshotNotFound = errors.New("repository: snapshot not found")

// SnapshotRepository persists market_snapshots rows: the 1-minute-bar raw
// market data and Feature Engine output produced each scan cycle
// (docs/architecture/er.md §market_snapshots).
type SnapshotRepository struct {
	db *sql.DB
}

// NewSnapshotRepository returns a SnapshotRepository backed by db.
func NewSnapshotRepository(db *sql.DB) *SnapshotRepository {
	return &SnapshotRepository{db: db}
}

// execer is satisfied by both *sql.DB and *sql.Tx, letting insertSnapshot
// run standalone or as part of a caller-managed transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const insertSnapshotSQL = `
INSERT INTO market_snapshots (
	instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover,
	return_1m, return_5m, return_15m, vwap, price_vs_vwap_bps, volume_ratio_5m,
	orderbook_imbalance, realized_vol_5m, market_return_5m, sector_return_5m,
	raw_data_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// Insert writes a single snapshot row.
func (r *SnapshotRepository) Insert(ctx context.Context, s domain.Snapshot) (domain.Snapshot, error) {
	return insertSnapshot(ctx, r.db, s)
}

// InsertBatch writes every snapshot in the given cycle to market_snapshots
// inside a single transaction (docs/architecture/er.md §market_snapshots
// "運用上の注意": 周期実行1サイクル分をまとめて1トランザクションで書き込み、
// SQLiteのWAL書き込みコストを抑える). Either every snapshot is committed, or
// none are: the transaction is rolled back on the first error, including a
// UNIQUE (instrument_id, timestamp) violation from a duplicate bar.
func (r *SnapshotRepository) InsertBatch(ctx context.Context, snapshots []domain.Snapshot) ([]domain.Snapshot, error) {
	if len(snapshots) == 0 {
		return nil, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("repository: begin snapshot batch transaction: %w", err)
	}

	out := make([]domain.Snapshot, 0, len(snapshots))
	for _, s := range snapshots {
		inserted, err := insertSnapshot(ctx, tx, s)
		if err != nil {
			return nil, errors.Join(err, tx.Rollback())
		}
		out = append(out, inserted)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("repository: commit snapshot batch transaction: %w", err)
	}
	return out, nil
}

func insertSnapshot(ctx context.Context, exec execer, s domain.Snapshot) (domain.Snapshot, error) {
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	res, err := exec.ExecContext(ctx, insertSnapshotSQL,
		s.InstrumentID, s.Symbol, formatTime(s.Timestamp), s.Price,
		nullableFloat64(s.Bid), nullableFloat64(s.Ask), nullableFloat64(s.SpreadBps),
		s.Volume, s.Turnover,
		nullableFloat64(s.Feature.Return1m), nullableFloat64(s.Feature.Return5m), nullableFloat64(s.Feature.Return15m),
		s.Feature.VWAP, s.Feature.PriceVsVWAPBps, nullableFloat64(s.Feature.VolumeRatio5m),
		nullableFloat64(s.Feature.OrderbookImbalance), nullableFloat64(s.Feature.RealizedVol5m),
		nullableFloat64(s.Feature.MarketReturn5m), nullableFloat64(s.Feature.SectorReturn5m),
		s.RawDataJSON, formatTime(createdAt),
	)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf(
			"repository: insert market snapshot for instrument %d at %s: %w", s.InstrumentID, s.Timestamp, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("repository: read snapshot id for instrument %d: %w", s.InstrumentID, err)
	}

	s.ID = id
	s.CreatedAt = createdAt
	return s, nil
}

// Get returns the snapshot with the given id, or ErrSnapshotNotFound.
func (r *SnapshotRepository) Get(ctx context.Context, id int64) (domain.Snapshot, error) {
	row := r.db.QueryRowContext(ctx, snapshotSelectColumns+` FROM market_snapshots WHERE id = ?`, id)
	return scanSnapshot(row)
}

// ListByInstrument returns up to limit snapshots for instrumentID, most
// recent first, for return/VWAP-slope style feature calculations and the
// Symbol Detail UI.
func (r *SnapshotRepository) ListByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx,
		snapshotSelectColumns+` FROM market_snapshots WHERE instrument_id = ? ORDER BY timestamp DESC LIMIT ?`,
		instrumentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list snapshots for instrument %d: %w", instrumentID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list snapshots for instrument %d: %w", instrumentID, err)
	}
	return out, nil
}

// ListByInstrumentRange returns every snapshot for instrumentID
// timestamped in the half-open range [from, to), ascending by timestamp
// (oldest first). Unlike ListByInstrument (most recent first, capped by
// limit, for UI/lookback use), this ordering and unlimited row count is
// what a Walk Forward backtest replay needs to feed
// internal/service/backtest one bar at a time in chronological order
// (functional.md FR-BT-2/FR-BT-3).
func (r *SnapshotRepository) ListByInstrumentRange(ctx context.Context, instrumentID int64, from, to time.Time) ([]domain.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx,
		snapshotSelectColumns+` FROM market_snapshots WHERE instrument_id = ? AND timestamp >= ? AND timestamp < ? ORDER BY timestamp ASC`,
		instrumentID, formatTime(from), formatTime(to),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list snapshots for instrument %d in [%s, %s): %w", instrumentID, from, to, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list snapshots for instrument %d in [%s, %s): %w", instrumentID, from, to, err)
	}
	return out, nil
}

const snapshotSelectColumns = `
SELECT id, instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover,
	return_1m, return_5m, return_15m, vwap, price_vs_vwap_bps, volume_ratio_5m,
	orderbook_imbalance, realized_vol_5m, market_return_5m, sector_return_5m,
	raw_data_json, created_at`

func scanSnapshot(row rowScanner) (domain.Snapshot, error) {
	var (
		s         domain.Snapshot
		timestamp string
		createdAt string
	)

	err := row.Scan(
		&s.ID, &s.InstrumentID, &s.Symbol, &timestamp, &s.Price,
		nullFloat(&s.Bid), nullFloat(&s.Ask), nullFloat(&s.SpreadBps),
		&s.Volume, &s.Turnover,
		nullFloat(&s.Feature.Return1m), nullFloat(&s.Feature.Return5m), nullFloat(&s.Feature.Return15m),
		&s.Feature.VWAP, &s.Feature.PriceVsVWAPBps, nullFloat(&s.Feature.VolumeRatio5m),
		nullFloat(&s.Feature.OrderbookImbalance), nullFloat(&s.Feature.RealizedVol5m),
		nullFloat(&s.Feature.MarketReturn5m), nullFloat(&s.Feature.SectorReturn5m),
		&s.RawDataJSON, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Snapshot{}, ErrSnapshotNotFound
	}
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("repository: scan snapshot: %w", err)
	}

	s.Timestamp, err = parseTime(timestamp)
	if err != nil {
		return domain.Snapshot{}, err
	}
	s.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Snapshot{}, err
	}
	return s, nil
}
