package calibration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// labeledSamplesQuery selects the directional trader outcomes
// ListLabeledSamples* aggregate; the callers append the optional horizon
// and timestamp conditions.
const labeledSamplesQuery = `
SELECT d.direction, d.confidence, o.horizon_minutes, o.future_return, o.was_direction_correct
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL`

// horizonCondition returns the SQL condition restricting
// o.horizon_minutes to horizons (with its args), or "" when horizons is
// empty (no restriction).
func horizonCondition(horizons []int) (string, []any) {
	if len(horizons) == 0 {
		return "", nil
	}
	args := make([]any, len(horizons))
	for i, h := range horizons {
		args[i] = h
	}
	return " AND o.horizon_minutes IN (?" + strings.Repeat(",?", len(horizons)-1) + ")", args
}

func (r *CalibrationRepository) queryLabeledSamples(ctx context.Context, query string, args []any) ([]domain.LabeledSample, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.LabeledSample
	for rows.Next() {
		var s domain.LabeledSample
		if err := rows.Scan(&s.Direction, &s.Confidence, &s.HorizonMinutes, &s.FutureReturn, &s.WasDirectionCorrect); err != nil {
			return nil, fmt.Errorf("repository: scan labeled calibration sample: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples: %w", err)
	}
	return out, nil
}

// ListLabeledSamples returns every calibration_outcomes row whose Jev
// decision is a directional trader call (decision_type=trader, direction
// LONG or SHORT), joined with that decision's Direction/Confidence, for
// internal/service/calibration.Metrics to aggregate into
// domain.CalibrationMetrics (functional.md FR-CAL-2/3). Direction=NONE
// decisions (WasDirectionCorrect always NULL) are excluded: there is no
// predicted direction to grade.
//
// Only outcomes whose horizon_minutes is in horizons are returned, so a
// per-horizon Calibration view never mixes e.g. a 5-minute label with a
// legacy 20-minute one (issue #719); an empty horizons applies no horizon
// restriction.
func (r *CalibrationRepository) ListLabeledSamples(ctx context.Context, horizons []int) ([]domain.LabeledSample, error) {
	cond, args := horizonCondition(horizons)
	return r.queryLabeledSamples(ctx, labeledSamplesQuery+cond, args)
}

// ListLabeledSamplesSince is ListLabeledSamples restricted to decisions
// timestamped at or after since: internal/service/selfimprove.Governor's
// daily Sol analysis (FR-SELFIMPROVE-1) evaluates only "直近の...
// Calibration指標", not the full historical dataset ListLabeledSamples
// itself serves (Calibration screen's all-time Reliability Curve).
func (r *CalibrationRepository) ListLabeledSamplesSince(ctx context.Context, since time.Time, horizons []int) ([]domain.LabeledSample, error) {
	cond, args := horizonCondition(horizons)
	return r.queryLabeledSamples(ctx,
		labeledSamplesQuery+" AND d.timestamp >= ?"+cond,
		append([]any{sqlutil.FormatTime(since)}, args...),
	)
}
