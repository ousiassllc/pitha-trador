package metalabel

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Retrain skip reason codes (metalabel_retrain_skips.reason_code; issue #799).
// They are fixed codes: readers (the Calibration screen, the MCP) never depend
// on a free-text message.
const (
	// SkipTooFewRowsBeforeOOS: fewer usable labeled decisions before the OOS
	// window than a fit and a calibration set need together.
	SkipTooFewRowsBeforeOOS = "too_few_rows_before_oos"
	// SkipTooFewRowsAfterPurge: the fit or the calibration set is below its
	// minimum once purge and embargo removed the overlapping rows.
	SkipTooFewRowsAfterPurge = "too_few_rows_after_purge"
	// SkipTrainingDataUnusable: enough rows, but they cannot be learned from
	// (for example every label is the same class).
	SkipTrainingDataUnusable = "training_data_unusable"
)

// RetrainSkip mirrors one metalabel_retrain_skips row: a nightly retraining
// that stored no model for lack of usable labeled history.
type RetrainSkip struct {
	ID int64
	// SkippedAt is when the retraining decided to skip.
	SkippedAt time.Time
	// ReasonCode is one of the Skip* codes.
	ReasonCode string
	// UsableRows is how many usable labeled decisions existed before the OOS
	// window; RequiredRows how many the training needs ("必要130件中 x件").
	UsableRows, RequiredRows int
	CreatedAt                time.Time
}

// InsertRetrainSkip stores s and returns it with ID and CreatedAt set.
func (r *Repository) InsertRetrainSkip(ctx context.Context, s RetrainSkip) (RetrainSkip, error) {
	createdAt := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `INSERT INTO metalabel_retrain_skips
		(skipped_at, reason_code, usable_rows, required_rows, created_at) VALUES (?, ?, ?, ?, ?)`,
		sqlutil.FormatTime(s.SkippedAt), s.ReasonCode, s.UsableRows, s.RequiredRows, sqlutil.FormatTime(createdAt))
	if err != nil {
		return RetrainSkip{}, fmt.Errorf("repository: insert meta-label retrain skip: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return RetrainSkip{}, fmt.Errorf("repository: meta-label retrain skip id: %w", err)
	}
	s.ID = id
	s.CreatedAt = createdAt
	return s, nil
}

// ListRetrainSkips returns the newest limit skips, newest SkippedAt first (the
// higher ID first on a tie).
func (r *Repository) ListRetrainSkips(ctx context.Context, limit int) ([]RetrainSkip, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, skipped_at, reason_code, usable_rows, required_rows, created_at
		FROM metalabel_retrain_skips ORDER BY skipped_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: list meta-label retrain skips: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RetrainSkip
	for rows.Next() {
		var (
			s                    RetrainSkip
			skippedAt, createdAt string
		)
		if err := rows.Scan(&s.ID, &skippedAt, &s.ReasonCode, &s.UsableRows, &s.RequiredRows, &createdAt); err != nil {
			return nil, fmt.Errorf("repository: scan meta-label retrain skip: %w", err)
		}
		if s.SkippedAt, err = sqlutil.ParseTime(skippedAt); err != nil {
			return nil, err
		}
		if s.CreatedAt, err = sqlutil.ParseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list meta-label retrain skips: %w", err)
	}
	return out, nil
}
