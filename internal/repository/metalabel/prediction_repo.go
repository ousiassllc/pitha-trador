package metalabel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// InsertPrediction stores p and returns it with ID and CreatedAt set. A model
// predicts a decision once: a second row for the same (ModelID, JevDecisionID)
// is an error (the unique index).
func (r *Repository) InsertPrediction(ctx context.Context, p Prediction) (Prediction, error) {
	createdAt := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `INSERT INTO metalabel_predictions (
		model_id, jev_decision_id, predicted_at, raw_probability, calibrated_probability,
		expected_value_bps, action, size_fraction, reason, features_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ModelID, p.JevDecisionID, sqlutil.FormatTime(p.PredictedAt), p.RawProbability, p.CalibratedProbability,
		p.ExpectedValueBps, p.Action, p.SizeFraction, p.Reason, p.FeaturesJSON, sqlutil.FormatTime(createdAt))
	if err != nil {
		return Prediction{}, fmt.Errorf("repository: insert meta-label prediction (model %d, decision %d): %w", p.ModelID, p.JevDecisionID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Prediction{}, fmt.Errorf("repository: meta-label prediction (model %d, decision %d) id: %w", p.ModelID, p.JevDecisionID, err)
	}
	p.ID = id
	p.CreatedAt = createdAt
	return p, nil
}

// PredictionForDecision returns modelID's prediction for jevDecisionID, or
// (zero, false, nil) when the model did not score that decision.
func (r *Repository) PredictionForDecision(ctx context.Context, modelID, jevDecisionID int64) (Prediction, bool, error) {
	var (
		p                    Prediction
		predictedAt, created string
	)
	err := r.db.QueryRowContext(ctx, `SELECT id, model_id, jev_decision_id, predicted_at, raw_probability,
		calibrated_probability, expected_value_bps, action, size_fraction, reason, features_json, created_at
		FROM metalabel_predictions WHERE model_id = ? AND jev_decision_id = ?`, modelID, jevDecisionID).
		Scan(&p.ID, &p.ModelID, &p.JevDecisionID, &predictedAt, &p.RawProbability, &p.CalibratedProbability,
			&p.ExpectedValueBps, &p.Action, &p.SizeFraction, &p.Reason, &p.FeaturesJSON, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Prediction{}, false, nil
	}
	if err != nil {
		return Prediction{}, false, fmt.Errorf("repository: read meta-label prediction (model %d, decision %d): %w", modelID, jevDecisionID, err)
	}
	if p.PredictedAt, err = sqlutil.ParseTime(predictedAt); err != nil {
		return Prediction{}, false, err
	}
	if p.CreatedAt, err = sqlutil.ParseTime(created); err != nil {
		return Prediction{}, false, err
	}
	return p, true, nil
}
