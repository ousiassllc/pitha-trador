package calibration

import (
	"context"
	"database/sql"
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
SELECT d.direction, d.confidence, o.horizon_minutes, o.future_return, o.was_direction_correct,
	o.barrier_hit, o.net_return_bps, o.entry_ref_price, o.label_version
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL`

// horizonCondition returns the SQL condition restricting
// o.horizon_minutes to horizons (with its args), or "" when horizons is
// empty (no restriction). With orTripleBarrier a triple-barrier label also
// matches whatever its horizon.
func horizonCondition(horizons []int, orTripleBarrier bool) (string, []any) {
	if len(horizons) == 0 {
		return "", nil
	}
	args := make([]any, len(horizons))
	for i, h := range horizons {
		args[i] = h
	}
	in := "o.horizon_minutes IN (?" + strings.Repeat(",?", len(horizons)-1) + ")"
	if orTripleBarrier {
		// A triple-barrier label's horizon is the Settings timeout (issue
		// #760), which need not be one of horizons.
		return fmt.Sprintf(" AND (%s OR o.label_version = %d)", in, domain.CalibrationLabelVersionTripleBarrier), args
	}
	return " AND " + in, args
}

func (r *CalibrationRepository) queryLabeledSamples(ctx context.Context, query string, args []any) ([]domain.LabeledSample, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.LabeledSample
	for rows.Next() {
		var (
			s          domain.LabeledSample
			barrierHit sql.NullString
		)
		if err := rows.Scan(&s.Direction, &s.Confidence, &s.HorizonMinutes, &s.FutureReturn, &s.WasDirectionCorrect,
			&barrierHit, sqlutil.NullFloat(&s.NetReturnBps), sqlutil.NullFloat(&s.EntryRefPrice), &s.LabelVersion); err != nil {
			return nil, fmt.Errorf("repository: scan labeled calibration sample: %w", err)
		}
		s.BarrierHit = barrierHit.String
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
	cond, args := horizonCondition(horizons, false)
	return r.queryLabeledSamples(ctx, labeledSamplesQuery+cond, args)
}

// ListCurrentLabeledSamples is ListLabeledSamples that also returns every
// triple-barrier label (label_version 2) whatever its horizon: that
// horizon is the Settings timeout (issue #760), which can be any number of
// minutes, while the horizons restriction still keeps legacy close-only
// labels of other horizons (e.g. the pre-#711 20 minutes) out.
func (r *CalibrationRepository) ListCurrentLabeledSamples(ctx context.Context, horizons []int) ([]domain.LabeledSample, error) {
	cond, args := horizonCondition(horizons, true)
	return r.queryLabeledSamples(ctx, labeledSamplesQuery+cond, args)
}

// ListLabeledSamplesSince is ListLabeledSamples restricted to decisions
// timestamped at or after since: internal/service/selfimprove.Governor's
// daily Sol analysis (FR-SELFIMPROVE-1) evaluates only "直近の...
// Calibration指標", not the full historical dataset ListLabeledSamples
// itself serves (Calibration screen's all-time Reliability Curve).
func (r *CalibrationRepository) ListLabeledSamplesSince(ctx context.Context, since time.Time, horizons []int) ([]domain.LabeledSample, error) {
	return r.listLabeledSamplesSince(ctx, since, horizons, false)
}

// ListCurrentLabeledSamplesSince is ListLabeledSamplesSince that also
// returns every triple-barrier label, as ListCurrentLabeledSamples.
func (r *CalibrationRepository) ListCurrentLabeledSamplesSince(ctx context.Context, since time.Time, horizons []int) ([]domain.LabeledSample, error) {
	return r.listLabeledSamplesSince(ctx, since, horizons, true)
}

func (r *CalibrationRepository) listLabeledSamplesSince(ctx context.Context, since time.Time, horizons []int, orTripleBarrier bool) ([]domain.LabeledSample, error) {
	cond, args := horizonCondition(horizons, orTripleBarrier)
	return r.queryLabeledSamples(ctx,
		labeledSamplesQuery+" AND d.timestamp >= ?"+cond,
		append([]any{sqlutil.FormatTime(since)}, args...),
	)
}
