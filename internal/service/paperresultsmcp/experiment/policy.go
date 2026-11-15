package experiment

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// policyChanges lists the Self-Improvement policy proposals applied or rolled
// back in [from, to) (SQL text bounds), the applied ones first and each group oldest first, as confounders of the
// given period (issue #879). Applying one moves the Policy Engine's thresholds
// (and the policy_version of every signal after it: "policy-v2+sol-12"), and
// rolling it back moves them back, so the two periods no longer differ only in
// the experiment's variable. An experiment never varies the policy this way, so
// none of them is exempt. The record is policy_proposals itself
// (applied_policy_version, applied_at, rolled_back_at): a signal-based check
// would only see a version once a signal carried it.
func policyChanges(ctx context.Context, q query.Q, period, from, to string) ([]Confounder, error) {
	out := []Confounder{}
	for _, c := range []struct{ what, column string }{{PolicyApplied, "applied_at"}, {PolicyRolledBack, "rolled_back_at"}} {
		// column is one of the two constants above, never caller text.
		rows, err := q.QueryContext(ctx, `
SELECT applied_policy_version, `+c.column+` FROM policy_proposals
WHERE `+c.column+` >= ? AND `+c.column+` < ? ORDER BY `+c.column+`, id`, from, to)
		if err != nil {
			return nil, fmt.Errorf("experiment: policy proposals: %w", err)
		}
		for rows.Next() {
			var version *string
			var at string
			if err := rows.Scan(&version, &at); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("experiment: scan policy proposal: %w", err)
			}
			f := Confounder{Kind: ConfounderPolicy, Period: period, At: query.FormatOut(at), Values: []string{c.what}}
			if version != nil {
				f.Name = *version
			}
			out = append(out, f)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, fmt.Errorf("experiment: policy proposals: %w", err)
		}
	}
	return out, nil
}
