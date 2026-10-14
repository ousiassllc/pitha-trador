package metalabel

import (
	"context"
	"errors"
	"fmt"
	"math"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// ErrInvalid is wrapped by the errors Store returns for a model or prediction
// that must not be stored; nothing is written in that case.
var ErrInvalid = errors.New("metalabel: invalid input")

// Store saves trained meta-labeling models with their OOS metrics and records
// each decision's prediction.
type Store struct {
	repo *repo.Repository
}

// NewStore returns a Store backed by r.
func NewStore(r *repo.Repository) *Store {
	return &Store{repo: r}
}

// SaveModel validates and stores m, returning it with ID and CreatedAt set. A
// rejected model is stored too (its OOS metrics and RejectReason stay on
// record) but LatestActiveModel never returns it.
func (s *Store) SaveModel(ctx context.Context, m repo.Model) (repo.Model, error) {
	if err := validateModel(m); err != nil {
		return repo.Model{}, err
	}
	return s.repo.InsertModel(ctx, m)
}

// LatestActiveModel returns the newest model that passed the OOS bar, or
// (zero, false, nil) when there is none. The caller MUST then skip the signal.
func (s *Store) LatestActiveModel(ctx context.Context) (repo.Model, bool, error) {
	return s.repo.LatestActiveModel(ctx)
}

// RecordPrediction validates and stores p, returning it with ID and CreatedAt
// set. A model records one prediction per Jev decision.
func (s *Store) RecordPrediction(ctx context.Context, p repo.Prediction) (repo.Prediction, error) {
	if err := validatePrediction(p); err != nil {
		return repo.Prediction{}, err
	}
	return s.repo.InsertPrediction(ctx, p)
}

// PredictionForDecision returns modelID's prediction for jevDecisionID, or
// (zero, false, nil) when the model did not score that decision.
func (s *Store) PredictionForDecision(ctx context.Context, modelID, jevDecisionID int64) (repo.Prediction, bool, error) {
	return s.repo.PredictionForDecision(ctx, modelID, jevDecisionID)
}

func validateModel(m repo.Model) error {
	switch {
	case m.Version == "":
		return invalid("model version is empty")
	case m.Algorithm == "":
		return invalid("model %q: algorithm is empty", m.Version)
	case m.Status != repo.ModelActive && m.Status != repo.ModelRejected:
		return invalid("model %q: status %q is neither %q nor %q", m.Version, m.Status, repo.ModelActive, repo.ModelRejected)
	case m.TrainedAt.IsZero() || m.TrainStart.IsZero() || m.TrainEnd.IsZero() || m.OOSStart.IsZero() || m.OOSEnd.IsZero():
		return invalid("model %q: trained_at and the training/OOS windows are required", m.Version)
	case m.TrainEnd.Before(m.TrainStart) || m.OOSEnd.Before(m.OOSStart):
		return invalid("model %q: a window ends before it starts", m.Version)
	case m.OOSStart.Before(m.TrainEnd):
		return invalid("model %q: the OOS window starts before the training window ends (information leak)", m.Version)
	case m.TrainSamples < 0 || m.OOSSamples < 0:
		return invalid("model %q: sample counts must not be negative", m.Version)
	case m.OOSAUC != nil && !(*m.OOSAUC >= 0 && *m.OOSAUC <= 1):
		return invalid("model %q: OOS AUC %v is outside [0, 1]", m.Version, *m.OOSAUC)
	case m.OOSExpectedValueBps != nil && !finite(*m.OOSExpectedValueBps):
		return invalid("model %q: OOS expected value is not finite", m.Version)
	case m.ParamsJSON == "":
		return invalid("model %q: params_json is empty", m.Version)
	}
	return nil
}

func validatePrediction(p repo.Prediction) error {
	switch {
	case p.ModelID <= 0 || p.JevDecisionID <= 0:
		return invalid("prediction needs a model and a Jev decision (model %d, decision %d)", p.ModelID, p.JevDecisionID)
	case p.PredictedAt.IsZero():
		return invalid("prediction (model %d, decision %d): predicted_at is required", p.ModelID, p.JevDecisionID)
	case !inUnit(p.RawProbability) || !inUnit(p.CalibratedProbability):
		return invalid("prediction (model %d, decision %d): probabilities must be within [0, 1]", p.ModelID, p.JevDecisionID)
	case !finite(p.ExpectedValueBps):
		return invalid("prediction (model %d, decision %d): expected value is not finite", p.ModelID, p.JevDecisionID)
	case p.Action != repo.ActionTake && p.Action != repo.ActionSkip:
		return invalid("prediction (model %d, decision %d): action %q is neither take nor skip", p.ModelID, p.JevDecisionID, p.Action)
	case !inUnit(p.SizeFraction):
		return invalid("prediction (model %d, decision %d): size fraction must be within [0, 1]", p.ModelID, p.JevDecisionID)
	case p.Action == repo.ActionSkip && p.SizeFraction != 0:
		return invalid("prediction (model %d, decision %d): a skip must have size fraction 0", p.ModelID, p.JevDecisionID)
	case p.Action == repo.ActionTake && p.SizeFraction == 0:
		return invalid("prediction (model %d, decision %d): a take needs a positive size fraction", p.ModelID, p.JevDecisionID)
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func inUnit(f float64) bool { return f >= 0 && f <= 1 }
