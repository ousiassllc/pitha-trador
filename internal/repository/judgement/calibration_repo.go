package judgement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// ErrCalibrationOutcomeNotFound is returned by CalibrationRepository
// methods when no matching calibration_outcomes row exists.
var ErrCalibrationOutcomeNotFound = errors.New("repository: calibration outcome not found")

// CalibrationRepository persists calibration_outcomes rows: Outcome
// Labeling's realized future_return/max_adverse_excursion/
// max_favorable_excursion/was_direction_correct for each Jev trader
// decision at each judgment horizon (docs/architecture/er.md
// §calibration_outcomes, functional.md §4.12).
type CalibrationRepository struct {
	db *sql.DB
}

// NewCalibrationRepository returns a CalibrationRepository backed by db.
func NewCalibrationRepository(db *sql.DB) *CalibrationRepository {
	return &CalibrationRepository{db: db}
}

const insertCalibrationOutcomeSQL = `
INSERT INTO calibration_outcomes (
	jev_decision_id, horizon_minutes, future_return, max_adverse_excursion,
	max_favorable_excursion, was_direction_correct, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`

// Insert writes a single calibration_outcomes row (internal/service/
// calibration.Labeler.HandleJob, FR-CAL-4). A duplicate
// (jev_decision_id, horizon_minutes) pair returns the UNIQUE constraint
// violation as-is: each pair is labeled at most once.
func (r *CalibrationRepository) Insert(ctx context.Context, o domain.CalibrationOutcome) (domain.CalibrationOutcome, error) {
	createdAt := o.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	res, err := r.db.ExecContext(ctx, insertCalibrationOutcomeSQL,
		o.JevDecisionID, o.HorizonMinutes, o.FutureReturn, o.MaxAdverseExcursion,
		o.MaxFavorableExcursion, sqlutil.NullableBool(o.WasDirectionCorrect), sqlutil.FormatTime(createdAt),
	)
	if err != nil {
		return domain.CalibrationOutcome{}, fmt.Errorf(
			"repository: insert calibration outcome for decision %d (horizon %dm): %w", o.JevDecisionID, o.HorizonMinutes, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.CalibrationOutcome{}, fmt.Errorf(
			"repository: read calibration outcome id for decision %d (horizon %dm): %w", o.JevDecisionID, o.HorizonMinutes, err)
	}

	o.ID = id
	o.CreatedAt = createdAt
	return o, nil
}

const calibrationOutcomeSelectColumns = `
SELECT id, jev_decision_id, horizon_minutes, future_return, max_adverse_excursion,
	max_favorable_excursion, was_direction_correct, created_at`

// Get returns the calibration_outcomes row with the given id, or
// ErrCalibrationOutcomeNotFound.
func (r *CalibrationRepository) Get(ctx context.Context, id int64) (domain.CalibrationOutcome, error) {
	row := r.db.QueryRowContext(ctx, calibrationOutcomeSelectColumns+` FROM calibration_outcomes WHERE id = ?`, id)
	return scanCalibrationOutcome(row)
}

func scanCalibrationOutcome(row sqlutil.RowScanner) (domain.CalibrationOutcome, error) {
	var (
		o         domain.CalibrationOutcome
		createdAt string
	)
	err := row.Scan(
		&o.ID, &o.JevDecisionID, &o.HorizonMinutes, &o.FutureReturn, &o.MaxAdverseExcursion,
		&o.MaxFavorableExcursion, sqlutil.NullBool(&o.WasDirectionCorrect), &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CalibrationOutcome{}, ErrCalibrationOutcomeNotFound
	}
	if err != nil {
		return domain.CalibrationOutcome{}, fmt.Errorf("repository: scan calibration outcome: %w", err)
	}
	o.CreatedAt, err = sqlutil.ParseTime(createdAt)
	if err != nil {
		return domain.CalibrationOutcome{}, err
	}
	return o, nil
}

// PendingLabel identifies one (jev_decision_id, horizon_minutes) pair
// whose Jev trader decision's horizon has elapsed but has no
// calibration_outcomes row yet (functional.md FR-CAL-4), for internal/
// service/scheduler's periodic outcome-labeling enqueue trigger.
type PendingLabel struct {
	JevDecisionID     int64
	InstrumentID      int64
	Symbol            string
	HorizonMinutes    int
	DecisionTimestamp time.Time
}

// OutcomeLabelJobPayload is the outcome-labeling queue job payload
// (JobQueueOutcomeLabeling): it identifies which jev_decisions row and
// horizon internal/service/calibration.Labeler.HandleJob computes and
// persists a calibration_outcomes row for. Defined in this package
// (rather than internal/service/calibration) so internal/service/
// scheduler - which must not depend on other internal/service
// sub-packages (doc.go) - can construct it directly when enqueuing.
type OutcomeLabelJobPayload struct {
	JevDecisionID  int64 `json:"jev_decision_id"`
	HorizonMinutes int   `json:"horizon_minutes"`
}

// PendingLabels returns every (jev_decision_id, horizon_minutes) pair
// among horizons whose Jev trader decision's timestamp+horizon has
// elapsed as of asOf but has no calibration_outcomes row yet, oldest
// decision first within each horizon.
func (r *CalibrationRepository) PendingLabels(ctx context.Context, horizons []int, asOf time.Time) ([]PendingLabel, error) {
	var out []PendingLabel
	for _, horizon := range horizons {
		labels, err := r.pendingLabelsForHorizon(ctx, horizon, asOf)
		if err != nil {
			return nil, err
		}
		out = append(out, labels...)
	}
	return out, nil
}

func (r *CalibrationRepository) pendingLabelsForHorizon(ctx context.Context, horizon int, asOf time.Time) ([]PendingLabel, error) {
	cutoff := asOf.Add(-time.Duration(horizon) * time.Minute)
	rows, err := r.db.QueryContext(ctx, `
SELECT d.id, d.instrument_id, d.symbol, d.timestamp
FROM jev_decisions d
WHERE d.decision_type = 'trader'
  AND d.timestamp <= ?
  AND NOT EXISTS (
    SELECT 1 FROM calibration_outcomes o
    WHERE o.jev_decision_id = d.id AND o.horizon_minutes = ?
  )
ORDER BY d.timestamp ASC`,
		sqlutil.FormatTime(cutoff), horizon,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list pending outcome labels for horizon %dm: %w", horizon, err)
	}
	defer func() { _ = rows.Close() }()

	var out []PendingLabel
	for rows.Next() {
		var (
			p         PendingLabel
			timestamp string
		)
		if err := rows.Scan(&p.JevDecisionID, &p.InstrumentID, &p.Symbol, &timestamp); err != nil {
			return nil, fmt.Errorf("repository: scan pending outcome label: %w", err)
		}
		p.HorizonMinutes = horizon
		p.DecisionTimestamp, err = sqlutil.ParseTime(timestamp)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list pending outcome labels for horizon %dm: %w", horizon, err)
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
func (r *CalibrationRepository) ListLabeledSamples(ctx context.Context) ([]domain.LabeledSample, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT d.direction, d.confidence, o.future_return, o.was_direction_correct
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.LabeledSample
	for rows.Next() {
		var s domain.LabeledSample
		if err := rows.Scan(&s.Direction, &s.Confidence, &s.FutureReturn, &s.WasDirectionCorrect); err != nil {
			return nil, fmt.Errorf("repository: scan labeled calibration sample: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples: %w", err)
	}
	return out, nil
}

// ListLabeledSamplesSince is ListLabeledSamples restricted to decisions
// timestamped at or after since: internal/service/selfimprove.Governor's
// daily Sol analysis (FR-SELFIMPROVE-1) evaluates only "直近の...
// Calibration指標", not the full historical dataset ListLabeledSamples
// itself serves (Calibration screen's all-time Reliability Curve).
func (r *CalibrationRepository) ListLabeledSamplesSince(ctx context.Context, since time.Time) ([]domain.LabeledSample, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT d.direction, d.confidence, o.future_return, o.was_direction_correct
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL
	AND d.timestamp >= ?`,
		sqlutil.FormatTime(since),
	)
	if err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples since %s: %w", since, err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.LabeledSample
	for rows.Next() {
		var s domain.LabeledSample
		if err := rows.Scan(&s.Direction, &s.Confidence, &s.FutureReturn, &s.WasDirectionCorrect); err != nil {
			return nil, fmt.Errorf("repository: scan labeled calibration sample: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list labeled calibration samples since %s: %w", since, err)
	}
	return out, nil
}
