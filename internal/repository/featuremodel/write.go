package featuremodel

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Run is one row of research_model_runs; the JSON columns are already encoded.
type Run struct {
	Version, Algorithm, LabelHorizon                string
	FeaturesJSON, HyperparamsJSON, CoefficientsJSON string
	TrainFrom, TrainTo, PredictDay                  string
	NTrain                                          int
	TrainDigest                                     string
}

// Prediction is one row of research_model_predictions: the model's predicted net
// bps of both sides for one Jev decision, and the direction it would take.
type Prediction struct {
	DecisionID, InstrumentID int64
	At                       time.Time
	Day                      string
	Long, Short              float64
	Direction                string
}

// InsertRun writes the run and all its predictions in ONE transaction. A run whose
// version exists already is left as it is, with no prediction written (inserted is
// false): re-running a day writes nothing new. n_predicted is len(preds).
func (r *Repository) InsertRun(ctx context.Context, run Run, preds []Prediction, createdAt time.Time) (inserted bool, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("featuremodel: begin run %s: %w", run.Version, err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO research_model_runs (version, algorithm, label_horizon, features_json, hyperparams_json,
	coefficients_json, train_from, train_to, predict_day, n_train, n_predicted, train_digest, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.Version, run.Algorithm, run.LabelHorizon, run.FeaturesJSON, run.HyperparamsJSON, run.CoefficientsJSON,
		run.TrainFrom, run.TrainTo, run.PredictDay, run.NTrain, len(preds), run.TrainDigest, sqlutil.FormatTime(createdAt))
	if err != nil {
		return false, fmt.Errorf("featuremodel: insert run %s: %w", run.Version, err)
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return false, fmt.Errorf("featuremodel: run id of %s: %w", run.Version, err)
	}
	for _, p := range preds {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO research_model_predictions (run_id, jev_decision_id, instrument_id, snapshot_at, day,
	pred_long_net_bps, pred_short_net_bps, direction, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID, p.DecisionID, p.InstrumentID, sqlutil.FormatTime(p.At), p.Day, p.Long, p.Short, p.Direction, sqlutil.FormatTime(createdAt)); err != nil {
			return false, fmt.Errorf("featuremodel: insert prediction of decision %d: %w", p.DecisionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("featuremodel: commit run %s: %w", run.Version, err)
	}
	return true, nil
}

// PurgePredictionsBefore deletes up to limit rows of research_model_predictions
// whose JST day is before `day`, in one short transaction, and returns how many.
// It touches no other table.
func (r *Repository) PurgePredictionsBefore(ctx context.Context, day string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM research_model_predictions WHERE id IN (
	SELECT id FROM research_model_predictions WHERE day < ? ORDER BY day LIMIT ?)`, day, limit)
	if err != nil {
		return 0, fmt.Errorf("featuremodel: purge predictions before %s: %w", day, err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// PurgeRunsBefore deletes up to limit rows of research_model_runs whose predicted
// day is before `day` and that have no prediction left, and returns how many.
// The NOT EXISTS keeps a run whose predictions are still being purged.
func (r *Repository) PurgeRunsBefore(ctx context.Context, day string, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM research_model_runs WHERE id IN (
	SELECT id FROM research_model_runs WHERE predict_day < ? AND NOT EXISTS (
		SELECT 1 FROM research_model_predictions p WHERE p.run_id = research_model_runs.id) ORDER BY predict_day LIMIT ?)`, day, limit)
	if err != nil {
		return 0, fmt.Errorf("featuremodel: purge runs before %s: %w", day, err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
