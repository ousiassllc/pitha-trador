package featuremodel

import (
	"context"
	"fmt"
	"math"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/gbt"
)

// Production values of the tree model's hyperparameters. Like the ridge model's they
// are constants: a change is a new model version (the 1 in rm-gbt1-...) and is
// recorded in every run's hyperparams_json. TreeParams carries them so tests can use
// small fixtures.
const (
	// TreeTrees is the number of boosting rounds, TreeDepth the most splits from a
	// root to a leaf, TreeRate the shrinkage of every tree and TreeMinLeaf the fewest
	// rows a leaf holds.
	TreeTrees   = 60
	TreeDepth   = 3
	TreeRate    = 0.1
	TreeMinLeaf = 100
	// TreeBins is the most quantile bins per feature.
	TreeBins = 32
	// TreeMaxRows bounds the rows the tree model holds in memory (the ridge model
	// streams its rows and holds none): above it every k-th row, in the fixed scan
	// order, is used, k being the smallest stride that fits.
	TreeMaxRows = 200000
)

// TreeParams are the hyperparameters of the tree model on top of Params (which
// gives the sample, the window and the minimum rows). DefaultTreeParams is the
// production set; only tests use another.
type TreeParams struct {
	Trees, Depth int
	Rate         float64
	MinLeaf      int
	Bins         int
	MaxRows      int
}

// DefaultTreeParams returns the production hyperparameters of the tree model.
func DefaultTreeParams() TreeParams {
	return TreeParams{Trees: TreeTrees, Depth: TreeDepth, Rate: TreeRate, MinLeaf: TreeMinLeaf, Bins: TreeBins, MaxRows: TreeMaxRows}
}

func (tp TreeParams) gbt() gbt.Params {
	return gbt.Params{Trees: tp.Trees, Depth: tp.Depth, Rate: tp.Rate, MinLeaf: tp.MinLeaf, Bins: tp.Bins}
}

// TreeModel is the fitted pair of boosted tree ensembles (LONG and SHORT net bps).
// Mean is the training mean of each feature over the observed values: a missing
// value is imputed with it before the trees see the vector.
type TreeModel struct {
	Mean  map[string]float64 `json:"mean"`
	Long  gbt.Model          `json:"long"`
	Short gbt.Model          `json:"short"`
	// StrideRows is the k of TreeMaxRows (1: every row was used).
	StrideRows int `json:"stride_rows"`
	NLong      int `json:"n_long"`
	NShort     int `json:"n_short"`
}

// Predict returns the predicted LONG and SHORT net bps of a feature vector aligned
// with repo.Features (NaN: missing).
func (m TreeModel) Predict(x []float64) (long, short float64) {
	z := make([]float64, len(repo.Features))
	for i, name := range repo.Features {
		z[i] = x[i]
		if math.IsNaN(x[i]) {
			z[i] = m.Mean[name]
		}
	}
	return m.Long.Predict(z), m.Short.Predict(z)
}

// TrainTree fits the tree model that predicts the JST day predictDay from the labels
// of one horizon, on the same window, purge and sample as TrainRidge (scanWindow: no
// row whose label was not fully known before predictDay began). The window's rows are
// read in the fixed order (day, snapshot_id), thinned deterministically to at most
// tp.MaxRows, imputed with the training mean and fitted without any randomness: the
// same rows and settings give bit-identical trees.
func TrainTree(ctx context.Context, src Source, horizon, predictDay string, p Params, tp TreeParams) (*Trained, error) {
	w, err := scanWindow(ctx, src, horizon, predictDay, p)
	if err != nil {
		return nil, err
	}
	t := w.t
	stride := max(1, (t.NTrain+tp.MaxRows-1)/tp.MaxRows)
	m := &TreeModel{Mean: map[string]float64{}, StrideRows: stride}
	for i, name := range repo.Features {
		m.Mean[name] = w.mean[i]
	}
	var xl, xs [][]float64
	var yl, ys []float64
	dig := newDigest(Spec{AlgorithmTree, horizon}.prefix())
	n := 0
	_, err = src.ScanTraining(ctx, horizon, w.days, p.SampleBuckets, predictDay, func(r repo.TrainRow) error {
		dig.row(r)
		keep := n%stride == 0
		n++
		if !keep {
			return nil
		}
		x := make([]float64, len(r.X))
		for i, v := range r.X {
			x[i] = v
			if math.IsNaN(v) {
				x[i] = w.mean[i]
			}
		}
		if r.HasLong {
			xl, yl = append(xl, x), append(yl, r.LongNet)
		}
		if r.HasShort {
			xs, ys = append(xs, x), append(ys, r.ShortNet)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if n != t.NTrain {
		return nil, fmt.Errorf("featuremodel: the training rows changed while reading (%d then %d)", t.NTrain, n)
	}
	if len(xl) < 2*tp.MinLeaf || len(xs) < 2*tp.MinLeaf {
		return nil, fmt.Errorf("%w: %d LONG and %d SHORT rows are too few for leaves of %d rows", ErrInsufficient, len(xl), len(xs), tp.MinLeaf)
	}
	m.NLong, m.NShort = len(xl), len(xs)
	if m.Long, err = gbt.Fit(xl, yl, tp.gbt()); err != nil {
		return nil, fmt.Errorf("featuremodel: fit long trees: %w", err)
	}
	if m.Short, err = gbt.Fit(xs, ys, tp.gbt()); err != nil {
		return nil, fmt.Errorf("featuremodel: fit short trees: %w", err)
	}
	t.Tree, t.Digest = m, dig.sum()
	return t, nil
}

// treeHyperparams is research_model_runs.hyperparams_json of a tree model.
type treeHyperparams struct {
	LabelHorizon  string  `json:"label_horizon"`
	Trees         int     `json:"trees"`
	Depth         int     `json:"depth"`
	LearningRate  float64 `json:"learning_rate"`
	MinLeaf       int     `json:"min_leaf"`
	Bins          int     `json:"bins"`
	MaxRows       int     `json:"max_rows"`
	StrideRows    int     `json:"stride_rows"`
	SampleBuckets int     `json:"sample_buckets"`
	MinTrainRows  int     `json:"min_train_rows"`
	MinTrainDays  int     `json:"min_train_days"`
	MaxTrainDays  int     `json:"max_train_days"`
	Loss          string  `json:"loss"`
	Splits        string  `json:"splits"`
	Randomness    string  `json:"randomness"`
	Imputation    string  `json:"imputation"`
	Purge         string  `json:"purge"`
}

func treeHyperparamsJSON(p Params, tp TreeParams, t *Trained) (string, error) {
	return marshal(treeHyperparams{
		LabelHorizon: t.Horizon, Trees: tp.Trees, Depth: tp.Depth, LearningRate: tp.Rate, MinLeaf: tp.MinLeaf, Bins: tp.Bins,
		MaxRows: tp.MaxRows, StrideRows: t.Tree.StrideRows, SampleBuckets: p.SampleBuckets,
		MinTrainRows: p.MinTrainRows, MinTrainDays: p.MinTrainDays, MaxTrainDays: p.MaxTrainDays,
		Loss:       "squared error, one tree set per side",
		Splits:     "histogram on training quantile bins; the largest variance reduction wins, ties go to the lowest feature then the lowest bin",
		Randomness: "none (no sampling of rows or features, rows in (day, snapshot_id) order, so the same rows give the same trees)",
		Imputation: "training mean",
		Purge:      "rows whose exit_at is not before the start of the predicted JST day are dropped",
	})
}
