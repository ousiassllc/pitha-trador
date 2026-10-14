// Package decisionsnapshot reads each Jev decision of a period together with
// the market_snapshots bar (features) it was made on, for the decision-log
// export (issue #771). It spans the judgement and market resource groups, so
// it is its own package rather than part of either (sibling packages under
// internal/repository never import each other; same reason as decisiontrade).
package decisionsnapshot

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/snapshotcols"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Repository reads decision / snapshot pairs from the SQLite database.
type Repository struct {
	db *sql.DB
}

// New returns a Repository backed by db.
func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Pair is one jev_decisions row's market_snapshots bar. Snapshot.RawDataJSON
// is empty: the export wants the features, not the whole board.
type Pair struct {
	DecisionID int64
	Snapshot   domain.Snapshot
}

// eachInRangeSQL pairs every jev_decisions row of a period with the bar it
// was made on. Both Jev callers stamp the decision with the bar's own
// timestamp (internal/service/jev: state.Timestamp = snap.Timestamp), so
// (instrument_id, timestamp) is the key, served by
// market_snapshots_instrument_timestamp_idx.
//
// Both sides are subqueries: jev_decisions and market_snapshots share column
// names (ofi_short, ...), which the feature column list must not make
// ambiguous.
var eachInRangeSQL = `
SELECT d.did, m.*
FROM (SELECT id AS did, instrument_id AS iid, timestamp AS ts FROM jev_decisions WHERE timestamp >= ? AND timestamp < ?) d
JOIN (SELECT id, instrument_id, symbol, timestamp, price, bid, ask, spread_bps, volume, turnover,
	` + snapshotcols.Names() + `, created_at FROM market_snapshots) m
	ON m.instrument_id = d.iid AND m.timestamp = d.ts
ORDER BY d.ts ASC, d.did ASC`

// EachInRange calls fn for every jev_decisions row timestamped in
// [from, to) that has a market_snapshots bar for its instrument at its
// timestamp, ordered by decision timestamp then id. A decision whose bar was
// already purged by retention is simply absent. The first error fn returns
// stops the read and is returned wrapped (errors.Is / errors.As still match).
func (r *Repository) EachInRange(ctx context.Context, from, to time.Time, fn func(Pair) error) error {
	err := sqlutil.Each(ctx, r.db, scanPair, fn, eachInRangeSQL, sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return fmt.Errorf("decisionsnapshot: read pairs in [%s, %s): %w", from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), err)
	}
	return nil
}

func scanPair(row sqlutil.RowScanner) (Pair, error) {
	var (
		p         Pair
		s         domain.Snapshot
		timestamp string
		createdAt string
	)
	dest := []any{
		&p.DecisionID, &s.ID, &s.InstrumentID, &s.Symbol, &timestamp, &s.Price,
		sqlutil.NullFloat(&s.Bid), sqlutil.NullFloat(&s.Ask), sqlutil.NullFloat(&s.SpreadBps),
		&s.Volume, &s.Turnover,
	}
	dest = append(dest, snapshotcols.Dests(&s)...)
	dest = append(dest, &createdAt)
	if err := row.Scan(dest...); err != nil {
		return Pair{}, fmt.Errorf("decisionsnapshot: scan pair: %w", err)
	}

	var err error
	if s.Timestamp, err = sqlutil.ParseTime(timestamp); err != nil {
		return Pair{}, err
	}
	if s.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return Pair{}, err
	}
	p.Snapshot = s
	return p, nil
}
