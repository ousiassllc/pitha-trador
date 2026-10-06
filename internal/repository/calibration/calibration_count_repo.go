package calibration

import (
	"context"
	"fmt"
)

// CountLabeledSamplesInConfidenceRange counts the rows ListLabeledSamples
// would return whose decision Confidence lies in [low, high) - or in
// [low, high] when includeHigh - without loading them. It is the cheap
// "is this confidence bucket populated enough?" probe that
// internal/service/policy runs on every Jev trader job, where the full
// sample list and PnL join of calibration.Service.Metrics would grow with
// the whole labeling history.
func (r *CalibrationRepository) CountLabeledSamplesInConfidenceRange(ctx context.Context, low, high float64, includeHigh bool) (int, error) {
	upper := "d.confidence < ?"
	if includeHigh {
		upper = "d.confidence <= ?"
	}
	var n int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL
	AND d.confidence >= ? AND `+upper, low, high,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("repository: count labeled calibration samples in [%v, %v]: %w", low, high, err)
	}
	return n, nil
}
