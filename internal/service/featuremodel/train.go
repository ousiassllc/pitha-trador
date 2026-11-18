package featuremodel

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// ErrInsufficient is wrapped by the error Train returns when the history before the
// predicted day is too short to fit the model (too few complete label days or
// rows). It is the normal early-days condition, not a failure: no run is written.
var ErrInsufficient = errors.New("featuremodel: insufficient training data")

// Source is what Train reads (internal/repository/featuremodel.Repository).
type Source interface {
	TrainingDays(ctx context.Context, before string, limit int) ([]string, error)
	ScanTraining(ctx context.Context, days []string, buckets int, predictDay string, fn func(repo.TrainRow) error) (purged int, err error)
}

// Trained is the result of one walk-forward training.
type Trained struct {
	Model Model
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

// Train fits the model that predicts the JST day predictDay with the hyperparameters p. It reads only the
// complete label days strictly before predictDay (the most recent p.MaxTrainDays of
// them) and, of those, only rows whose label was fully known before predictDay began
// (exit_at < the start of predictDay): nothing from predictDay or later can enter,
// whatever those rows hold. The rows are read twice in the fixed order (day,
// snapshot_id) and never held: first for the feature means and deviations, then for
// the normal equations. The same rows give bit-identical coefficients.
func Train(ctx context.Context, src Source, predictDay string, p Params) (*Trained, error) {
	if _, err := time.ParseInLocation(time.DateOnly, predictDay, time.UTC); err != nil {
		return nil, fmt.Errorf("featuremodel: predicted day %q: %w", predictDay, err)
	}
	days, err := src.TrainingDays(ctx, predictDay, p.MaxTrainDays)
	if err != nil {
		return nil, err
	}
	if len(days) < p.MinTrainDays {
		return nil, fmt.Errorf("%w: %d complete label days before %s, need %d", ErrInsufficient, len(days), predictDay, p.MinTrainDays)
	}
	stats := make([]welford, len(repo.Features))
	t := &Trained{}
	seen := map[string]bool{}
	purged, err := src.ScanTraining(ctx, days, p.SampleBuckets, predictDay, func(r repo.TrainRow) error {
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
		return nil, fmt.Errorf("%w: %d days with rows, %d LONG and %d SHORT rows before %s (need %d days, %d rows per side)",
			ErrInsufficient, t.Days, t.NLong, t.NShort, predictDay, p.MinTrainDays, p.MinTrainRows)
	}
	m := Model{Mean: map[string]float64{}, Std: map[string]float64{}}
	mean, std := make([]float64, len(stats)), make([]float64, len(stats))
	for i := range stats {
		mean[i], std[i] = stats[i].meanStd()
		m.Mean[repo.Features[i]], m.Std[repo.Features[i]] = mean[i], std[i]
	}
	long, short := newNormalEq(len(stats)), newNormalEq(len(stats))
	dig := newDigest()
	z, n := make([]float64, len(stats)), 0
	_, err = src.ScanTraining(ctx, days, p.SampleBuckets, predictDay, func(r repo.TrainRow) error {
		n++
		for i, x := range r.X {
			z[i] = 0 // a missing value standardizes to the training mean
			if !math.IsNaN(x) {
				z[i] = (x - mean[i]) / std[i]
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

// digest hashes the ordered training rows: snapshot id, which sides exist, the
// targets and the feature values (a missing value as its own marker).
type digest struct {
	h   hash.Hash
	buf [8]byte
}

func newDigest() *digest {
	d := &digest{h: sha256.New()}
	_, _ = d.h.Write([]byte(VersionPrefix))
	return d
}

func (d *digest) u64(v uint64) {
	binary.BigEndian.PutUint64(d.buf[:], v)
	_, _ = d.h.Write(d.buf[:])
}

func (d *digest) f64(v float64, present bool) {
	if !present {
		_, _ = d.h.Write([]byte{0})
		return
	}
	_, _ = d.h.Write([]byte{1})
	d.u64(math.Float64bits(v))
}

func (d *digest) row(r repo.TrainRow) {
	_ = binary.Write(d.h, binary.BigEndian, r.SnapshotID) // hash.Hash.Write never fails
	d.f64(r.LongNet, r.HasLong)
	d.f64(r.ShortNet, r.HasShort)
	for _, x := range r.X {
		d.f64(x, !math.IsNaN(x))
	}
}

func (d *digest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }
