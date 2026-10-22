package training

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	// OOSRows of RequiredOOSRows: the rows in the OOS window against the rows
	// the window reaches back to hold (the bar's MinOOSSamples), so a skip shows
	// both sides of the split (issue #862). Set where the split fails.
	OOSRows, RequiredOOSRows *int
	// DroppedIncomplete, LabelV1, LabelV2 are filled by the Trainer: the stored
	// rows BuildRows dropped for a missing feature, and the labeled decisions of
	// the lookback per label version (v1 close-only, v2 triple-barrier). They tell
	// a thin history from rows that exist but cannot be used. All the counts are
	// nil while not measured (a failed count stays nil) and 0 only when measured
	// as 0; they are logged as "unknown" and stored as NULL when nil.
	DroppedIncomplete, LabelV1, LabelV2 *int
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
	slog.Warn("metalabel: nightly retraining skipped; the existing model (or a skip) stays in force",
		"reason", e, "code", e.Code,
		"dropped_incomplete", countText(e.DroppedIncomplete), "label_v1", countText(e.LabelV1), "label_v2", countText(e.LabelV2),
		"pre", e.Usable, "required_pre", e.Required, "oos", countText(e.OOSRows), "required_oos", countText(e.RequiredOOSRows))
	if t.skips == nil {
		return nil
	}
	err := t.skips.RecordRetrainSkip(ctx, repo.RetrainSkip{
		SkippedAt: t.now().UTC(), ReasonCode: e.Code, UsableRows: e.Usable, RequiredRows: e.Required,
		DroppedIncomplete: e.DroppedIncomplete, LabelV1Rows: e.LabelV1, LabelV2Rows: e.LabelV2,
		OOSRows: e.OOSRows, RequiredOOSRows: e.RequiredOOSRows,
	})
	if err != nil {
		return fmt.Errorf("metalabel: record retrain skip: %w", err)
	}
	return nil
}

// annotateSkip fills what a skip reports besides its own counts (issue #862):
// the rows BuildRows dropped and the labeled decisions of [from, to) per label
// version, which tell "no history yet" from "history that cannot be used" (the
// v1 rows have no order-flow features). Any other error passes through. When
// the counts cannot be read the skip is still recorded, with the version counts
// left unrecorded (nil, stored as NULL) and a warning: the breakdown is a diagnostic and must not turn a
// normal skip into a failing task.
func (t *Trainer) annotateSkip(ctx context.Context, err error, from, to time.Time, dropped int) error {
	e, ok := asInsufficient(err)
	if !ok {
		return err
	}
	counts, cerr := t.source.CountLabeledDecisions(ctx, from, to)
	if cerr != nil {
		slog.Warn("metalabel: could not count labeled decisions per label version; the skip is recorded without them", "error", cerr)
	}
	e.DroppedIncomplete = &dropped
	if cerr == nil {
		e.LabelV1, e.LabelV2 = &counts.V1, &counts.V2
	}
	return e
}

// countText is the log value of a breakdown count: the count, or "unknown"
// when it was not measured (stored as NULL, never as 0).
func countText(n *int) any {
	if n == nil {
		return "unknown"
	}
	return *n
}

// asInsufficient returns err's InsufficientDataError, if it is one.
func asInsufficient(err error) (*InsufficientDataError, bool) {
	var e *InsufficientDataError
	return e, errors.As(err, &e)
}
