package calibration

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// LabelSkip mirrors one calibration_label_skips row: a
// (decision, horizon) pair whose label window can never be completed
// (FR-CAL-4), the "no label, and why" half of the 後付けラベル.
type LabelSkip struct {
	JevDecisionID  int64
	HorizonMinutes int
	Reason         string
	CreatedAt      time.Time
}

// decisionRangeFilter selects the jev_decisions of [from, to) by timestamp,
// the period of the decision-log export (issue #771). Its two parameters are
// the formatted from/to. A subquery rather than a JOIN keeps the select
// columns of this package's outer table unambiguous.
const decisionRangeFilter = `jev_decision_id IN (SELECT id FROM jev_decisions WHERE timestamp >= ? AND timestamp < ?)`

// EachOutcomeByDecisionRange calls fn for every calibration_outcomes row of a
// jev_decisions row timestamped in [from, to), ordered by (jev_decision_id,
// horizon_minutes): the 後付けラベル half of the decision log. The first error
// fn returns stops the read and is returned wrapped.
func (r *CalibrationRepository) EachOutcomeByDecisionRange(ctx context.Context, from, to time.Time, fn func(domain.CalibrationOutcome) error) error {
	err := sqlutil.Each(ctx, r.db, scanCalibrationOutcome, fn,
		calibrationOutcomeSelectColumns+` FROM calibration_outcomes WHERE `+decisionRangeFilter+` ORDER BY jev_decision_id ASC, horizon_minutes ASC`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	return wrapDecisionRange(err, "calibration outcomes", from, to)
}

// EachLabelSkipByDecisionRange is EachOutcomeByDecisionRange for the
// calibration_label_skips marks: the (decision, horizon) pairs that will
// never get an outcome, and why.
func (r *CalibrationRepository) EachLabelSkipByDecisionRange(ctx context.Context, from, to time.Time, fn func(LabelSkip) error) error {
	err := sqlutil.Each(ctx, r.db, scanLabelSkip, fn,
		`SELECT jev_decision_id, horizon_minutes, reason, created_at FROM calibration_label_skips WHERE `+decisionRangeFilter+` ORDER BY jev_decision_id ASC, horizon_minutes ASC`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	return wrapDecisionRange(err, "calibration label skips", from, to)
}

func scanLabelSkip(row sqlutil.RowScanner) (LabelSkip, error) {
	var (
		s         LabelSkip
		createdAt string
	)
	if err := row.Scan(&s.JevDecisionID, &s.HorizonMinutes, &s.Reason, &createdAt); err != nil {
		return LabelSkip{}, fmt.Errorf("repository: scan calibration label skip: %w", err)
	}
	var err error
	if s.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
		return LabelSkip{}, fmt.Errorf("repository: parse calibration label skip %d/%dm created_at: %w", s.JevDecisionID, s.HorizonMinutes, err)
	}
	return s, nil
}

func wrapDecisionRange(err error, what string, from, to time.Time) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("repository: read %s of decisions in [%s, %s): %w", what, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339), err)
}
