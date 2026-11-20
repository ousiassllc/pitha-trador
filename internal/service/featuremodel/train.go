package featuremodel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// ErrInsufficient is wrapped by the error Train returns when the history before the
// predicted day is too short to fit the model (too few complete label days or
// rows). It is the normal early-days condition, not a failure: no run is written.
var ErrInsufficient = errors.New("featuremodel: insufficient training data")

// Source is what training reads (internal/repository/featuremodel.Repository).
type Source interface {
	TrainingDays(ctx context.Context, horizon, before string, limit int) ([]string, error)
	ScanTraining(ctx context.Context, horizon string, days []string, buckets int, predictDay string, fn func(repo.TrainRow) error) (purged int, err error)
}

// Trained is the result of one walk-forward training. Model is the ridge model (the
// zero value for a tree model); Tree is the tree model (nil for a ridge model).
type Trained struct {
	Model Model
	Tree  *TreeModel
	// Horizon is the label horizon the model learned.
	Horizon string
	// From and To are the first and last JST day that contributed rows; Days how many days did.
	From, To string
	Days     int
	// NTrain are the training rows (a row has at least one side); NLong and NShort
	// the rows each side was fitted on.
	NTrain, NLong, NShort int
	// Purged is how many rows the exit_at rule dropped.
	Purged int
	// Digest is the SHA-256 over the ordered training rows (identity and values).
	Digest string
}

// Train fits the baseline ridge model, the 30-minute label's: TrainRidge for Horizon.
func Train(ctx context.Context, src Source, predictDay string, p Params) (*Trained, error) {
	return TrainRidge(ctx, src, Horizon, predictDay, p)
}

// window is the walk-forward training window of one horizon as a first scan sees it:
// the days, the row counts and the feature means and deviations to standardize with.
type window struct {
	t         *Trained
	days      []string
	mean, std []float64
}

// scanWindow reads only the complete label days strictly before predictDay (the most
// recent p.MaxTrainDays of them) and, of those, only rows whose label was fully known
// before predictDay began (exit_at < the start of predictDay): nothing from
// predictDay or later can enter, whatever those rows hold, and this holds for every
// horizon alike (a label's exit is at most the close of its own day). The rows are
// read in the fixed order (day, snapshot_id) and never held.
func scanWindow(ctx context.Context, src Source, horizon, predictDay string, p Params) (*window, error) {
	if _, err := time.ParseInLocation(time.DateOnly, predictDay, time.UTC); err != nil {
		return nil, fmt.Errorf("featuremodel: predicted day %q: %w", predictDay, err)
	}
	days, err := src.TrainingDays(ctx, horizon, predictDay, p.MaxTrainDays)
	if err != nil {
		return nil, err
	}
	if len(days) < p.MinTrainDays {
		return nil, fmt.Errorf("%w: %d complete %s label days before %s, need %d", ErrInsufficient, len(days), horizon, predictDay, p.MinTrainDays)
	}
	stats := make([]welford, len(repo.Features))
	t := &Trained{Horizon: horizon}
	seen := map[string]bool{}
	purged, err := src.ScanTraining(ctx, horizon, days, p.SampleBuckets, predictDay, func(r repo.TrainRow) error {
		for i, x := range r.X {
			if !math.IsNaN(x) {
				stats[i].add(x)
			}
		}
		t.NTrain++
		if r.HasLong {
			t.NLong++
		}
		if r.HasShort {
			t.NShort++
		}
		if !seen[r.Day] {
			seen[r.Day] = true
			if t.From == "" {
				t.From = r.Day
			}
			t.To = r.Day
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	t.Purged, t.Days = purged, len(seen)
	if t.Days < p.MinTrainDays || t.NLong < p.MinTrainRows || t.NShort < p.MinTrainRows {
		return nil, fmt.Errorf("%w: %d days with %s rows, %d LONG and %d SHORT rows before %s (need %d days, %d rows per side)",
			ErrInsufficient, t.Days, horizon, t.NLong, t.NShort, predictDay, p.MinTrainDays, p.MinTrainRows)
	}
	w := &window{t: t, days: days, mean: make([]float64, len(stats)), std: make([]float64, len(stats))}
	for i := range stats {
		w.mean[i], w.std[i] = stats[i].meanStd()
	}
	return w, nil
}

// TrainRidge fits the ridge model that predicts the JST day predictDay from the
// labels of one horizon with the hyperparameters p (see scanWindow for what it may
// read). A second scan streams the rows into the normal equations. The same rows
// give bit-identical coefficients.
func TrainRidge(ctx context.Context, src Source, horizon, predictDay string, p Params) (*Trained, error) {
	w, err := scanWindow(ctx, src, horizon, predictDay, p)
	if err != nil {
		return nil, err
	}
	t := w.t
	m := Model{Mean: map[string]float64{}, Std: map[string]float64{}}
	for i, name := range repo.Features {
		m.Mean[name], m.Std[name] = w.mean[i], w.std[i]
	}
	long, short := newNormalEq(len(w.mean)), newNormalEq(len(w.mean))
	dig := newDigest(Spec{Algorithm, horizon}.prefix())
	z, n := make([]float64, len(w.mean)), 0
	_, err = src.ScanTraining(ctx, horizon, w.days, p.SampleBuckets, predictDay, func(r repo.TrainRow) error {
		n++
		for i, x := range r.X {
			z[i] = 0 // a missing value standardizes to the training mean
			if !math.IsNaN(x) {
				z[i] = (x - w.mean[i]) / w.std[i]
			}
		}
		if r.HasLong {
			long.add(z, r.LongNet)
		}
		if r.HasShort {
			short.add(z, r.ShortNet)
		}
		dig.row(r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if n != t.NTrain || long.n != t.NLong || short.n != t.NShort {
		return nil, fmt.Errorf("featuremodel: the training rows changed while reading (%d then %d)", t.NTrain, n)
	}
	if m.Long, err = fit(long, "long", p.RidgeLambda); err != nil {
		return nil, err
	}
	if m.Short, err = fit(short, "short", p.RidgeLambda); err != nil {
		return nil, err
	}
	t.Model, t.Digest = m, dig.sum()
	return t, nil
}

// fit solves one side and names its coefficients.
func fit(e *normalEq, side string, lambda float64) (Side, error) {
	w, err := e.solve(lambda)
	if err != nil {
		return Side{}, fmt.Errorf("featuremodel: fit %s: %w", side, err)
	}
	s := Side{N: e.n, Intercept: w[0], Coef: map[string]float64{}}
	for i, name := range repo.Features {
		s.Coef[name] = w[i+1]
	}
	return s, nil
}
