package featuremodel

import (
	"context"
	"fmt"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// DecisionSource is what predictions read: the trader LONG/SHORT decisions of a
// JST day with their own bar's features.
type DecisionSource interface {
	Decisions(ctx context.Context, day string) ([]repo.Decision, error)
}

// Predictor is a fitted model: the predicted LONG and SHORT net bps of a feature
// vector aligned with repo.Features (NaN: missing). Model and TreeModel are.
type Predictor interface {
	Predict(x []float64) (long, short float64)
}

// Predictor returns the fitted model of the training, ridge or tree.
func (t *Trained) Predictor() Predictor {
	if t.Tree != nil {
		return t.Tree
	}
	return t.Model
}

// Predict returns the model's prediction for every decision of the day that has
// a bar (the source returns only those), in decision-id order.
func Predict(ctx context.Context, src DecisionSource, m Predictor, day string) ([]repo.Prediction, error) {
	ds, err := src.Decisions(ctx, day)
	if err != nil {
		return nil, err
	}
	preds := make([]repo.Prediction, 0, len(ds))
	for _, d := range ds {
		long, short := m.Predict(d.X)
		preds = append(preds, repo.Prediction{DecisionID: d.ID, InstrumentID: d.InstrumentID, At: d.At, Day: day, Long: long, Short: short, Direction: Direction(long, short)})
	}
	return preds, nil
}

// runRow builds the research_model_runs row of a training of the model spec that
// predicts predictDay: its features, hyperparameters and fitted coefficients (or trees).
func runRow(spec Spec, predictDay string, t *Trained, p Params, tp TreeParams) (repo.Run, error) {
	features, err := marshal(repo.Features)
	if err != nil {
		return repo.Run{}, err
	}
	var hp, coef string
	if t.Tree != nil {
		if hp, err = treeHyperparamsJSON(p, tp, t); err == nil {
			coef, err = marshal(t.Tree)
		}
	} else if hp, err = hyperparamsJSON(p, spec.Horizon); err == nil {
		coef, err = marshal(t.Model)
	}
	if err != nil {
		return repo.Run{}, fmt.Errorf("featuremodel: run record: %w", err)
	}
	return repo.Run{
		Version: spec.Version(predictDay), Algorithm: spec.Algorithm, LabelHorizon: spec.Horizon,
		FeaturesJSON: features, HyperparamsJSON: hp, CoefficientsJSON: coef,
		TrainFrom: t.From, TrainTo: t.To, PredictDay: predictDay, NTrain: t.NTrain, TrainDigest: t.Digest,
	}, nil
}
