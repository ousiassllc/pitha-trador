package metalabel

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// listSkippedLabelRows returns the counterfactual labels of skipped candidates
// (counterfactual_labels, issue #1052) in the same shape and scope as
// ListTrainingRowsFrom: LONG/SHORT trader decisions made in [from, to), the
// newest label of each decision. Only called when the training includes skipped
// labels (TrainingScope.IncludeSkippedLabels). A decision that has a
// triple-barrier calibration_outcomes row with a net return is left out, so it
// can never be a row twice: the row ListTrainingRowsFrom reads is the one used.
// Labels recorded while the LLM cutoff was unverified (cutoff_unverified = 1: a
// date, but no recorded Jev model) are kept in the table and left out here, so
// neither the training nor the out-of-sample evaluation, which split these
// rows, ever sees them.
func listSkippedLabelRows(ctx context.Context, q Querier, from, to time.Time, scope TrainingScope) ([]TrainingRow, error) {
	scopeCond, scopeArgs := scope.Where()
	if scopeCond != "" {
		scopeCond = "\n\tAND " + scopeCond
	}
	args := append([]any{domain.CalibrationLabelVersionTripleBarrier}, scopeArgs...)
	args = append(args, sqlutil.FormatTime(from), sqlutil.FormatTime(to), domain.CalibrationLabelVersionTripleBarrier, domain.CalibrationLabelVersionTripleBarrier)
	//nolint:gosec // G202: scopeCond is a package-internal NOT IN fragment; every value is a bound parameter
	rows, err := q.QueryContext(ctx, `
SELECT d.id, d.symbol, d.model_id, d.timestamp, d.direction, d.state_json, d.response_json,
	d.ofi_short, d.ofi_mid, d.ofi_long, d.microprice_dev_bps,
	c.horizon_minutes, c.net_return_bps
FROM counterfactual_labels c
JOIN jev_decisions d ON d.id = c.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
	AND c.label_version = ? AND c.cutoff_unverified = 0`+scopeCond+`
	AND d.timestamp >= ? AND d.timestamp < ?
	AND NOT EXISTS (SELECT 1 FROM calibration_outcomes o
		WHERE o.jev_decision_id = d.id AND o.label_version = ? AND o.net_return_bps IS NOT NULL)
	AND c.id = (SELECT MAX(c2.id) FROM counterfactual_labels c2
		WHERE c2.jev_decision_id = d.id AND c2.label_version = ?)
ORDER BY d.timestamp, d.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list skipped-candidate training rows in [%s, %s): %w", from, to, err)
	}
	return collectTrainingRows(rows, true)
}

// mergeByDecisionTime merges two row lists, each ordered by (decision time,
// decision id), into one list in that order.
func mergeByDecisionTime(a, b []TrainingRow) []TrainingRow {
	out := make([]TrainingRow, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if before(b[j], a[i]) {
			out = append(out, b[j])
			j++
			continue
		}
		out = append(out, a[i])
		i++
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}

func before(x, y TrainingRow) bool {
	if !x.DecidedAt.Equal(y.DecidedAt) {
		return x.DecidedAt.Before(y.DecidedAt)
	}
	return x.JevDecisionID < y.JevDecisionID
}
