package calibration

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// countLabeledSamplesQuery counts the rows ListLabeledSamples would
// return whose decision Confidence lies in the range (the first %s picks the
// upper bound's comparison, the second adds the prompt-version condition or
// nothing), stopping after the first ? matches. The
// (decision_type, direction, confidence) index
// jev_decisions_type_direction_confidence_idx turns the confidence range
// into an index range search and LIMIT caps the rows visited, so the cost
// is bounded by the limit rather than by the labeling history (issue
// #603; calibration_plan_test.go pins the plan).
const countLabeledSamplesQuery = `
SELECT COUNT(*) FROM (
	SELECT 1
	FROM jev_decisions d
	JOIN calibration_outcomes o ON o.jev_decision_id = d.id
	WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
		AND d.confidence >= ? AND d.%s%s
	LIMIT ?
)`

// CountLabeledSamplesInConfidenceRange counts the rows ListLabeledSamples
// would return whose decision Confidence lies in [low, high) - or in
// [low, high] when includeHigh - without loading them, capped at limit:
// it returns min(count, limit) and stops scanning once limit rows are
// found. It is the cheap "does this confidence bucket hold at least limit
// samples?" probe that internal/service/policy runs on every Jev trader
// job, where the full sample list and PnL join of
// calibration.Service.Metrics (or an unbounded COUNT) would grow with the
// whole labeling history. A limit <= 0 returns 0.
//
// Like ListLabeledSamples it counts the live prompt version's cohort only
// (SetForeignTraderVersions, issue #881): the samples of another cohort's
// versions are not this version's evidence. A failed read of the live version
// is an error. AllVersions is the same read over every version, which only
// Paper's fallback uses (docs/architecture/jev-prompt-versions-in-data.md §4).
func (r *CalibrationRepository) CountLabeledSamplesInConfidenceRange(ctx context.Context, low, high float64, includeHigh bool, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	upper := "confidence < ?"
	if includeHigh {
		upper = "confidence <= ?"
	}
	args, scope := []any{low, high}, ""
	if r.foreign != nil {
		versions, err := r.foreign(ctx)
		if err != nil {
			return 0, fmt.Errorf("repository: resolve the Trader prompt versions to leave out: %w", err)
		}
		cond, scopeArgs := sqlutil.NotIn("d.question_version", versions)
		if cond != "" {
			scope, args = " AND "+cond, append(args, scopeArgs...)
		}
	}
	var n int
	if err := r.db.QueryRowContext(ctx, fmt.Sprintf(countLabeledSamplesQuery, upper, scope), append(args, limit)...).Scan(&n); err != nil {
		return 0, fmt.Errorf("repository: count labeled calibration samples in [%v, %v]: %w", low, high, err)
	}
	return n, nil
}
