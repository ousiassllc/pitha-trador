// Package stalled finds the order proposals (issue #829) the application has
// not moved on, for health_today's order_proposal_stalled attention code. It
// has its own small query interface so that query (which holds health_today)
// can import it while proposal (which imports query) stays acyclic.
package stalled

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// After is how long an order proposal may stay received or processing before
// it counts as stalled. The application picks a proposal up within seconds
// (proposalexec.DefaultInterval). A proposal waiting for a session
// (waiting_session) or a submitted limit order waits by design and is never
// stalled.
const After = 10 * time.Minute

// Querier is the read-only connection (query.Q satisfies it).
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Proposals are the stalled order proposals: Count of them, the oldest one's
// last status change (RFC3339 UTC) in OldestSince, nil when there is none.
type Proposals struct {
	Count       int     `json:"stalled_count"`
	OldestSince *string `json:"oldest_stalled_since"`
}

// Find counts the proposals stuck in received or processing since before
// now - After (updated_at is when the status last changed).
func Find(ctx context.Context, q Querier, now time.Time) (Proposals, error) {
	var (
		out    Proposals
		oldest sql.NullString
	)
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*), MIN(updated_at) FROM order_proposals
WHERE status IN ('received', 'processing') AND updated_at < ?`,
		sqlutil.FormatTime(now.Add(-After))).Scan(&out.Count, &oldest)
	if err != nil {
		return Proposals{}, fmt.Errorf("stalled: order proposals: %w", err)
	}
	if oldest.Valid {
		t, err := sqlutil.ParseTime(oldest.String)
		if err != nil {
			return Proposals{}, fmt.Errorf("stalled: oldest updated_at: %w", err)
		}
		s := t.UTC().Format(time.RFC3339)
		out.OldestSince = &s
	}
	return out, nil
}
