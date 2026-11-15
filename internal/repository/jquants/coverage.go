package jquants

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// MarkCovered records that (dataset, day, code) was fetched completely, with the
// number of rows it wrote (rewriting an earlier mark). code is "" for the
// datasets that are not per issue. The backfill fetches only what has no mark.
func (r *Repository) MarkCovered(ctx context.Context, dataset, day, code string, rows int, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO research_jquants_coverage (dataset, day, code, row_count, completed_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (dataset, day, code) DO UPDATE SET row_count = excluded.row_count, completed_at = excluded.completed_at`,
		dataset, day, code, rows, sqlutil.FormatTime(at))
	if err != nil {
		return fmt.Errorf("jquants: mark %s %s %s covered: %w", dataset, day, code, err)
	}
	return nil
}

// Covered returns the codes with a mark for (dataset, day) ("" is the code of the
// datasets that are not per issue).
func (r *Repository) Covered(ctx context.Context, dataset, day string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code FROM research_jquants_coverage WHERE dataset = ? AND day = ?`, dataset, day)
	if err != nil {
		return nil, fmt.Errorf("jquants: list %s coverage of %s: %w", dataset, day, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("jquants: scan coverage: %w", err)
		}
		out[code] = true
	}
	return out, rows.Err()
}
