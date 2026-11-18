package featuremodel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
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
	// Day and Version are the JST day and the run version a run was written for ("" when
	// none), with its training rows and recorded predictions.
	Day, Version     string
	NTrain, NPredict int
	// Skipped are the run versions without enough training data (they are tried again at
	// the next Run).
	Skipped []string
	// Purged is how many old rows of the two model tables this run deleted.
	Purged int
	// More is true when this run stopped at its bound with work left: call Run again.
	// Every run with More set made progress.
	More bool
}

// Runner trains and records the models of every recent JST day that had trader
// decisions and have no run yet, one run (one model of one day) per Run, and purges
// old rows.
type Runner struct {
	store      Store
	params     Params
	treeParams TreeParams
	specs      []Spec
	now        func() time.Time
	purgeRows  int
	maxPurge   int
}

// NewRunner returns a Runner over store that trains the Baseline model only; the
// nightly task adds the others with WithSpecs(Specs()...).
func NewRunner(store Store) *Runner {
	return &Runner{store: store, params: DefaultParams(), treeParams: DefaultTreeParams(), specs: []Spec{Baseline}, now: time.Now, purgeRows: PurgeBatchRows, maxPurge: DefaultMaxPurgeBatches}
}

// WithSpecs sets the models the Runner trains, in the order they are tried within a day.
func (r *Runner) WithSpecs(specs ...Spec) *Runner { r.specs = specs; return r }

// WithTreeParams overrides DefaultTreeParams (tests, with small fixtures).
func (r *Runner) WithTreeParams(tp TreeParams) *Runner { r.treeParams = tp; return r }

// WithClock overrides time.Now (tests).
func (r *Runner) WithClock(now func() time.Time) *Runner { r.now = now; return r }

// WithParams overrides DefaultParams (tests, with small fixtures).
func (r *Runner) WithParams(p Params) *Runner { r.params = p; return r }

// WithPurgeBatches overrides PurgeBatchRows and DefaultMaxPurgeBatches (tests).
func (r *Runner) WithPurgeBatches(rows, batches int) *Runner {
	r.purgeRows, r.maxPurge = rows, batches
	return r
}

// pending lists the (day, model) runs still to do, oldest day first and, within a day,
// in the order of r.specs.
func (r *Runner) pending(ctx context.Context, from, to string) ([]work, error) {
	var todo []work
	for _, spec := range r.specs {
		if err := spec.validate(); err != nil {
			return nil, err
		}
		days, err := r.store.PendingDays(ctx, from, to, spec.Version)
		if err != nil {
			return nil, err
		}
		for _, day := range days {
			todo = append(todo, work{spec, day})
		}
	}
	slices.SortStableFunc(todo, func(a, b work) int { return strings.Compare(a.day, b.day) })
	return todo, nil
}

// work is one run to do: a model and the JST day it predicts.
type work struct {
	spec Spec
	day  string
}

// Run does one bounded chunk: the oldest run of the last LookbackDays days up to
// today (not older than the retention) that has trader LONG/SHORT decisions and no
// run is trained and predicted (runs without enough history are skipped and logged);
// then, once none is left, the rows older than retentionDays JST days are deleted
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
	todo, err := r.pending(ctx, from, today.Format(time.DateOnly))
	if err != nil {
		return res, err
	}
	for i, w := range todo {
		version := w.spec.Version(w.day)
		trained, preds, err := r.runOne(ctx, w)
		if errors.Is(err, ErrInsufficient) {
			slog.InfoContext(ctx, "featuremodel: no run for the day", "version", version, "reason", err.Error())
			res.Skipped = append(res.Skipped, version)
			continue
		}
		if err != nil {
			return res, err
		}
		res.Day, res.Version, res.NTrain, res.NPredict, res.More = w.day, version, trained.NTrain, preds, i+1 < len(todo)
		slog.InfoContext(ctx, "featuremodel: model trained and recorded", "version", version, "train_from", trained.From, "train_to", trained.To,
			"n_train", trained.NTrain, "purged_rows", trained.Purged, "predictions", preds)
		break
	}
	if res.More {
		return res, nil
	}
	res.Purged, res.More, err = r.purge(ctx, cutoff)
	return res, err
}

// runOne trains the model for the day and writes the run and its predictions in one
// transaction (nothing is written when the run exists already).
func (r *Runner) runOne(ctx context.Context, w work) (*Trained, int, error) {
	var (
		t   *Trained
		err error
	)
	if w.spec.Algorithm == AlgorithmTree {
		t, err = TrainTree(ctx, r.store, w.spec.Horizon, w.day, r.params, r.treeParams)
	} else {
		t, err = TrainRidge(ctx, r.store, w.spec.Horizon, w.day, r.params)
	}
	if err != nil {
		return nil, 0, err
	}
	preds, err := Predict(ctx, r.store, t.Predictor(), w.day)
	if err != nil {
		return nil, 0, err
	}
	run, err := runRow(w.spec, w.day, t, r.params, r.treeParams)
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
