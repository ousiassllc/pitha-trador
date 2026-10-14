package judgement

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// EachInRange calls fn for every jev_decisions row (Scout and Trader)
// timestamped in the half-open range [from, to), oldest first, for the
// decision-log export (issue #771). It streams: the period is never held in
// memory. The first error fn returns stops the read and is returned wrapped
// (errors.Is / errors.As still match it).
func (r *DecisionRepository) EachInRange(ctx context.Context, from, to time.Time, fn func(domain.JevDecision) error) error {
	err := sqlutil.Each(ctx, r.db, scanDecision, fn,
		decisionSelectColumns+` FROM jev_decisions WHERE timestamp >= ? AND timestamp < ? ORDER BY timestamp ASC, id ASC`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return fmt.Errorf("repository: read jev decisions in [%s, %s): %w", from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), err)
	}
	return nil
}
