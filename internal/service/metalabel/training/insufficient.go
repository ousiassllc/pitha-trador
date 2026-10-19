package training

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// InsufficientDataError is the error behind ErrInsufficientData: it carries the
// fixed reason code and the counts the nightly job records when it skips a
// retraining (issue #799), so no reader has to parse the message.
// errors.Is(err, ErrInsufficientData) holds for it.
type InsufficientDataError struct {
	// Code is one of the repo.Skip* codes.
	Code string
	// Usable of Required: the usable labeled decisions before the OOS window
	// (SkipTooFewRowsBeforeOOS), those counted toward the fit and calibration
	// minimums after purging (SkipTooFewRowsAfterPurge) or the rows that could
	// not be learned from (SkipTrainingDataUnusable), against what fit and
	// calibration need together ("必要130件中 x件").
	Usable, Required int
	// Detail is the human-readable message (logs only; never persisted).
	Detail string
	// Cause is the learner's error for SkipTrainingDataUnusable.
	Cause error
}

func (e *InsufficientDataError) Error() string {
	msg := ErrInsufficientData.Error() + ": " + e.Detail
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap lets errors.Is match ErrInsufficientData and the learner's error.
func (e *InsufficientDataError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrInsufficientData}
	}
	return []error{ErrInsufficientData, e.Cause}
}

// SkipRecorder persists a skipped retraining; *metalabel.Store implements it.
type SkipRecorder interface {
	RecordRetrainSkip(ctx context.Context, s repo.RetrainSkip) error
}

// WithSkipRecorder makes Retrain record every skip for lack of data (issue
// #799); without one a skip is only logged.
func WithSkipRecorder(r SkipRecorder) TrainerOption { return func(t *Trainer) { t.skips = r } }

// recordSkip logs the skip and, with a recorder, stores it. A failure to store
// is returned: the maintenance task then fails visibly instead of the skip
// vanishing.
func (t *Trainer) recordSkip(ctx context.Context, e *InsufficientDataError) error {
	slog.Warn("metalabel: nightly retraining skipped; the existing model (or a skip) stays in force", "reason", e)
	if t.skips == nil {
		return nil
	}
	err := t.skips.RecordRetrainSkip(ctx, repo.RetrainSkip{
		SkippedAt: t.now().UTC(), ReasonCode: e.Code, UsableRows: e.Usable, RequiredRows: e.Required,
	})
	if err != nil {
		return fmt.Errorf("metalabel: record retrain skip: %w", err)
	}
	return nil
}

// asInsufficient returns err's InsufficientDataError, if it is one.
func asInsufficient(err error) (*InsufficientDataError, bool) {
	var e *InsufficientDataError
	return e, errors.As(err, &e)
}
