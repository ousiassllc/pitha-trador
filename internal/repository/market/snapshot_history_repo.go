package market

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// maxHistoryBatchInstruments bounds the instrument ids bound into one
// ListHistoryByInstruments statement, below SQLite's default 32766 host
// parameter limit (one more parameter carries the offset).
const maxHistoryBatchInstruments = 30000

// ListHistoryByInstrument is ListByInstrument without raw_data_json: the
// feature/screener history readers (candidate refresh, market-data job,
// market context) never read the stored board, which is ~1-2KB per row.
// Snapshot.RawDataJSON is left empty. Most recent first.
func (r *SnapshotRepository) ListHistoryByInstrument(ctx context.Context, instrumentID int64, limit int) ([]domain.Snapshot, error) {
	got, err := r.ListHistoryByInstruments(ctx, []int64{instrumentID}, limit)
	if err != nil {
		return nil, err
	}
	return got[instrumentID], nil
}

// listHistoryByInstrumentsSQL has one %s: the IN placeholder list. For each
// instrument, `cut` seeks the timestamp of its limit-th most recent bar
// through the (instrument_id, timestamp) index (OFFSET limit-1; NULL when
// the instrument has fewer bars), and the join then range-scans only
// timestamp >= cut. Timestamps are unique per instrument
// (UNIQUE (instrument_id, timestamp)), so at most limit rows come back per
// instrument and no row outside the window is read.
const listHistoryByInstrumentsSQL = `
WITH cut AS (
	SELECT id AS iid,
		(SELECT timestamp FROM market_snapshots
			WHERE instrument_id = instruments.id ORDER BY timestamp DESC LIMIT 1 OFFSET ?) AS ts
	FROM instruments WHERE id IN (%s)
)
%s FROM cut JOIN market_snapshots ON instrument_id = cut.iid AND timestamp >= COALESCE(cut.ts, '')
ORDER BY instrument_id, timestamp DESC`

// ListHistoryByInstruments returns, for every id in instrumentIDs, up to
// limit most recent snapshots (most recent first) without raw_data_json, in
// one query rather than one per instrument (candidate refresh used to issue
// a ListByInstrument per active stock every cycle). Instruments without
// snapshots have no map entry.
func (r *SnapshotRepository) ListHistoryByInstruments(ctx context.Context, instrumentIDs []int64, limit int) (map[int64][]domain.Snapshot, error) {
	out := make(map[int64][]domain.Snapshot, len(instrumentIDs))
	if limit <= 0 {
		return out, nil
	}
	for start := 0; start < len(instrumentIDs); start += maxHistoryBatchInstruments {
		end := min(start+maxHistoryBatchInstruments, len(instrumentIDs))
		if err := r.listHistoryBatch(ctx, instrumentIDs[start:end], limit, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *SnapshotRepository) listHistoryBatch(ctx context.Context, ids []int64, limit int, out map[int64][]domain.Snapshot) error {
	args := make([]any, 0, len(ids)+1)
	args = append(args, limit-1)
	for _, id := range ids {
		args = append(args, id)
	}
	// G201: the only interpolated parts are "?" markers and constant column
	// names; ids and limit are bound parameters.
	query := fmt.Sprintf(listHistoryByInstrumentsSQL, //nolint:gosec // G201: see above
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), snapshotHistoryColumns)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("repository: list snapshot history for %d instruments: %w", len(ids), err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		s, err := scanSnapshotColumns(rows, false)
		if err != nil {
			return err
		}
		out[s.InstrumentID] = append(out[s.InstrumentID], s)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("repository: list snapshot history for %d instruments: %w", len(ids), err)
	}
	return nil
}
