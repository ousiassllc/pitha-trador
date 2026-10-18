package judgement

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// LastChangeAt returns when the policy.* thresholds were last changed by the
// self-improvement flow: the newest apply or automatic rollback of a proposal
// (issue #780: the change-rate limit and the freeze around a model adoption
// count both). It returns (zero, false, nil) when no proposal was ever applied.
func (r *ProposalRepository) LastChangeAt(ctx context.Context) (time.Time, bool, error) {
	var newest sql.NullString
	err := r.db.QueryRowContext(ctx, `
SELECT MAX(changed_at) FROM (
	SELECT applied_at AS changed_at FROM policy_proposals WHERE applied_at IS NOT NULL
	UNION ALL
	SELECT rolled_back_at FROM policy_proposals WHERE rolled_back_at IS NOT NULL
)`).Scan(&newest)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("repository: newest policy change: %w", err)
	}
	if !newest.Valid {
		return time.Time{}, false, nil
	}
	t, err := sqlutil.ParseTime(newest.String)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}
