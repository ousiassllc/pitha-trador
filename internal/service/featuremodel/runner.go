package featuremodel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// LookbackDays is how many JST days before today a day without a run is still
// trained for: a night the job missed is caught up within a week.
const LookbackDays = 7

// PurgeBatchRows is how many rows one DELETE removes, and DefaultMaxPurgeBatches how
// many such deletes one Run makes before it reports More and leaves the rest to the
// next Run.
const (
	PurgeBatchRows         = 1000
	DefaultMaxPurgeBatches = 10
)

// jst is the zone of the days the model works in.
var jst = time.FixedZone("JST", 9*60*60)

// Store is the persistence Runner needs (internal/repository/featuremodel).
type Store interface {
	Source
	DecisionSource
	PendingDays(ctx context.Context, from, to string, version func(day string) string) ([]string, error)
	InsertRun(ctx context.Context, run repo.Run, preds []repo.Prediction, createdAt time.Time) (bool, error)
	PurgePredictionsBefore(ctx context.Context, day string, limit int) (int, error)
	PurgeRunsBefore(ctx context.Context, day string, limit int) (int, error)
}

// Result is what one Run did.
type Result struct {
	// Day is the JST day a run was written for ("" when none), with its training rows
	// and recorded predictions.
	Day              string
	NTrain, NPredict int
	// Skipped are days without enough training data (they are tried again at the next Run).
	Skipped []string
	// Purged is how many old rows of the two model tables this run deleted.
	Purged int
	// More is true when this run stopped at its bound with work left: call Run again.
	// Every run with More set made progress.
	More bool
}

// Runner trains and records the model of every recent JST day that had trader
// decisions and has no run yet, one day per Run, and purges old rows.
type Runner struct {
	store     Store
	params    Params
	now       func() time.Time
	purgeRows int
	maxPurge  int
}

// NewRunner returns a Runner over store.
func NewRunner(store Store) *Runner {
	return &Runner{store: store, params: DefaultParams(), now: time.Now, purgeRows: PurgeBatchRows, maxPurge: DefaultMaxPurgeBatches}
}

// WithClock overrides time.Now (tests).
func (r *Runner) WithClock(now func() time.Time) *Runner { r.now = now; return r }

// WithParams overrides DefaultParams (tests, with small fixtures).
func (r *Runner) WithParams(p Params) *Runner { r.params = p; return r }

// WithPurgeBatches overrides PurgeBatchRows and DefaultMaxPurgeBatches (tests).
func (r *Runner) WithPurgeBatches(rows, batches int) *Runner {
	r.purgeRows, r.maxPurge = rows, batches
	return r
}

// Run does one bounded chunk: the oldest day of the last LookbackDays days up to
// today (not older than the retention) that has trader LONG/SHORT decisions and no
// run is trained and predicted (days without enough history are skipped and logged);
// then, once no day is left, the rows older than retentionDays JST days are deleted
// (predictions first, then runs, in batches). The caller decides whether the day is
// over (the nightly job runs from 16:00 JST) and whether the model is switched on.
func (r *Runner) Run(ctx context.Context, retentionDays int) (Result, error) {
	var res Result
	today, err := time.ParseInLocation(time.DateOnly, r.now().In(jst).Format(time.DateOnly), jst)
	if err != nil {
		return res, err
	}
	cutoff := today.AddDate(0, 0, -retentionDays).Format(time.DateOnly)
	from := today.AddDate(0, 0, -LookbackDays).Format(time.DateOnly)
	if from < cutoff {
		from = cutoff // a day older than the retention would be purged at once
	}
	days, err := r.store.PendingDays(ctx, from, today.Format(time.DateOnly), Version)
	if err != nil {
		return res, err
	}
	for i, day := range days {
		trained, preds, err := r.runDay(ctx, day)
		if errors.Is(err, ErrInsufficient) {
			slog.InfoContext(ctx, "featuremodel: no run for the day", "day", day, "reason", err.Error())
			res.Skipped = append(res.Skipped, day)
			continue
		}
		if err != nil {
			return res, err
		}
		res.Day, res.NTrain, res.NPredict, res.More = day, trained.NTrain, preds, i+1 < len(days)
		slog.InfoContext(ctx, "featuremodel: model trained and recorded", "day", day, "train_from", trained.From, "train_to", trained.To,
			"n_train", trained.NTrain, "purged_rows", trained.Purged, "predictions", preds)
		break
	}
	if res.More {
		return res, nil
	}
	res.Purged, res.More, err = r.purge(ctx, cutoff)
	return res, err
}

// runDay trains for the day and writes the run and its predictions in one
// transaction (nothing is written when the run exists already).
func (r *Runner) runDay(ctx context.Context, day string) (*Trained, int, error) {
	t, err := Train(ctx, r.store, day, r.params)
	if err != nil {
		return nil, 0, err
	}
	preds, err := Predict(ctx, r.store, t.Model, day)
	if err != nil {
		return nil, 0, err
	}
	run, err := runRow(day, t, r.params)
	if err != nil {
		return nil, 0, err
	}
	if _, err := r.store.InsertRun(ctx, run, preds, r.now()); err != nil {
		return nil, 0, err
	}
	return t, len(preds), nil
}

// purge deletes the model rows of days before `before` in batches: all old
// predictions first, then the runs left without any, each step at most maxPurge
// batches. more is true when a step stopped at its bound with a full last batch.
func (r *Runner) purge(ctx context.Context, before string) (deleted int, more bool, err error) {
	steps := []func(context.Context, string, int) (int, error){r.store.PurgePredictionsBefore, r.store.PurgeRunsBefore}
	for _, step := range steps {
		for batch := 1; ; batch++ {
			if err := ctx.Err(); err != nil {
				return deleted, false, err
			}
			n, err := step(ctx, before, r.purgeRows)
			deleted += n
			if err != nil {
				return deleted, false, fmt.Errorf("featuremodel: %w", err)
			}
			if n < r.purgeRows {
				break
			}
			if batch == r.maxPurge {
				return deleted, true, nil
			}
		}
	}
	return deleted, false, nil
}
