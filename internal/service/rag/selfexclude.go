package rag

import (
	"context"
	"fmt"
	"strings"
)

// selfMargin is how many extra nearest neighbours searchExcluding fetches
// beyond k so the subject's own rows (which sit at the very top of the KNN
// order: same symbol, near-identical vector) can be dropped without a
// second round trip. It covers the current Scout/Trader decisions and the
// ~15 one-minute snapshots inside SnapshotRecencyGuard; if more own rows
// than that exist the search is widened (selfWidenFactor) until k rows
// survive or the table is exhausted, so the result never depends on it.
const (
	selfMargin       = 32
	selfWidenFactor  = 4
	selfLookupMaxIDs = 500 // stay well under SQLite's bound-variable limit
)

// searchExcluding is search minus the rows self describes: it fetches
// k+selfMargin neighbours, drops the own rows, and returns at most k of the
// rest in distance order. filter/filterArgs are the optional vec0 id
// constraint of search (kept for the cheap labeled-decision subset).
func (s *Service) searchExcluding(ctx context.Context, table, idColumn, filter string, filterArgs []any, v Vector, k int, self selfRows) ([]match, error) {
	if !self.active() {
		return s.search(ctx, table, idColumn, filter, filterArgs, v, k)
	}
	for limit := k + selfMargin; ; limit *= selfWidenFactor {
		raw, err := s.search(ctx, table, idColumn, filter, filterArgs, v, limit)
		if err != nil {
			return nil, err
		}
		kept, err := s.dropSelf(ctx, raw, self)
		if err != nil {
			return nil, err
		}
		if len(kept) >= k || len(raw) < limit {
			if len(kept) > k {
				kept = kept[:k]
			}
			return kept, nil
		}
	}
}

// dropSelf removes from matches the ids that are rows of self, keeping order.
func (s *Service) dropSelf(ctx context.Context, matches []match, self selfRows) ([]match, error) {
	own := make(map[int64]bool)
	for start := 0; start < len(matches); start += selfLookupMaxIDs {
		batch := matches[start:min(start+selfLookupMaxIDs, len(matches))]
		args := make([]any, 0, len(batch)+len(self.args))
		for _, m := range batch {
			args = append(args, m.id)
		}
		args = append(args, self.args...)
		query := `SELECT id FROM ` + self.table + ` WHERE id IN (?` + strings.Repeat(",?", len(batch)-1) + `) AND ` + self.cond //nolint:gosec // G202: table/cond are package-internal constants (never user input); values are bound parameters
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("rag: look up own %s rows: %w", self.table, err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("rag: scan own %s row: %w", self.table, err)
			}
			own[id] = true
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, fmt.Errorf("rag: iterate own %s rows: %w", self.table, err)
		}
	}
	kept := make([]match, 0, len(matches))
	for _, m := range matches {
		if !own[m.id] {
			kept = append(kept, m)
		}
	}
	return kept, nil
}
