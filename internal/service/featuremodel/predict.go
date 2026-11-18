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

// Predict returns the model's prediction for every decision of the day that has
// a bar (the source returns only those), in decision-id order.
func Predict(ctx context.Context, src DecisionSource, m Model, day string) ([]repo.Prediction, error) {
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

// runRow builds the research_model_runs row of a training and its n predictions.
func runRow(predictDay string, t *Trained, p Params) (repo.Run, error) {
	features, err := marshal(repo.Features)
	if err != nil {
		return repo.Run{}, err
	}
	hp, err := hyperparamsJSON(p)
	if err != nil {
		return repo.Run{}, err
	}
	coef, err := marshal(t.Model)
	if err != nil {
		return repo.Run{}, fmt.Errorf("featuremodel: coefficients: %w", err)
	}
	return repo.Run{
		Version: Version(predictDay), Algorithm: Algorithm, LabelHorizon: Horizon,
		FeaturesJSON: features, HyperparamsJSON: hp, CoefficientsJSON: coef,
		TrainFrom: t.From, TrainTo: t.To, PredictDay: predictDay, NTrain: t.NTrain, TrainDigest: t.Digest,
	}, nil
}
