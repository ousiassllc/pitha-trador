package query

import (
	"context"
	"fmt"
)

// RetrainSkip is one metalabel_history retrain skip: a nightly retraining
// that stored no model for lack of usable labeled history (issue #799).
// ReasonCode is one of the fixed codes below, never free text.
type RetrainSkip struct {
	SkippedAt    string `json:"skipped_at"`
	ReasonCode   string `json:"reason_code"`
	UsableRows   int64  `json:"usable_rows"`
	RequiredRows int64  `json:"required_rows"`
}

// retrainSkipCodeOther is returned for a stored code this build does not know.
const retrainSkipCodeOther = "other"

// retrainSkipCodes are the codes of internal/repository/metalabel (Skip*)
// metalabel_history may return; anything else becomes "other".
var retrainSkipCodes = map[string]bool{
	"too_few_rows_before_oos":  true,
	"too_few_rows_after_purge": true,
	"training_data_unusable":   true,
}

// NormalizeRetrainSkipCode maps a stored code this build does not know to "other".
func NormalizeRetrainSkipCode(code string) string {
	if !retrainSkipCodes[code] {
		return retrainSkipCodeOther
	}
	return code
}

// retrainSkips returns up to limit retrain skips decided in r, newest first.
func retrainSkips(ctx context.Context, q Q, r Range, limit int) ([]RetrainSkip, error) {
	from, to := r.bounds()
	rows, err := q.QueryContext(ctx, `
SELECT skipped_at, reason_code, usable_rows, required_rows
FROM metalabel_retrain_skips
WHERE skipped_at >= ? AND skipped_at < ?
ORDER BY skipped_at DESC, id DESC
LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query: metalabel retrain skips: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []RetrainSkip{}
	for rows.Next() {
		var (
			s  RetrainSkip
			at string
		)
		if err := rows.Scan(&at, &s.ReasonCode, &s.UsableRows, &s.RequiredRows); err != nil {
			return nil, fmt.Errorf("query: scan metalabel retrain skip: %w", err)
		}
		s.SkippedAt = FormatOut(at)
		s.ReasonCode = NormalizeRetrainSkipCode(s.ReasonCode)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: metalabel retrain skips: %w", err)
	}
	return out, nil
}
