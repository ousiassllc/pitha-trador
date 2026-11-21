package training

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training/walkforward"
)

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
func (t *Trainer) recordSkip(ctx context.Context, e *walkforward.InsufficientDataError) error {
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
func asInsufficient(err error) (*walkforward.InsufficientDataError, bool) {
	var e *walkforward.InsufficientDataError
	return e, errors.As(err, &e)
}
