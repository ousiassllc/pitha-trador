package featuremodel_test

import (
	"context"
	"database/sql"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// seedModelRun inserts a run of predictDay with n predictions of that day.
func seedModelRun(t *testing.T, db *sql.DB, predictDay string, n int) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO research_model_runs (version, algorithm, label_horizon, features_json, hyperparams_json, coefficients_json, train_from, train_to, predict_day,
	n_train, n_predicted, train_digest) VALUES (?, 'ridge', '30m', '[]', '{}', '{}', '2026-01-01', '2026-01-02', ?, 1, ?, 'x')`, "v-"+predictDay, predictDay, n)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	for i := 0; i < n; i++ {
		if _, err := db.Exec(`INSERT INTO research_model_predictions (run_id, jev_decision_id, instrument_id, snapshot_at, day, pred_long_net_bps, pred_short_net_bps, direction)
VALUES (?, ?, 1, 't', ?, 0, 0, 'NONE')`, id, 1000+i, predictDay); err != nil {
			t.Fatal(err)
		}
	}
}

// The retention purge removes only model rows of days before today - retention
// (predictions first, then the runs left without any), in bounded chunks that say More
// until done, and touches nothing else: not the labels, their marks, the decisions.
func TestRunnerPurgesOnlyOldModelRowsInChunks(t *testing.T) {
	ctx := context.Background()
	db := modeltest.HistoryDB(t, "2026-10-05", 6, 20)
	modeltest.SeedDecision(t, db, modeltest.At(t, "2026-10-05", "10:00", 1), "trader", "LONG")
	seedModelRun(t, db, "2026-06-01", 25) // old: retention 30 from 2026-10-12 keeps 2026-09-12 on
	seedModelRun(t, db, "2026-07-01", 3)  // old
	seedModelRun(t, db, "2026-09-12", 4)  // exactly at the cutoff: kept
	seedModelRun(t, db, "2026-10-10", 2)
	untouched := func() [4]int {
		return [4]int{
			count(t, db, `SELECT COUNT(*) FROM research_horizon_labels`), count(t, db, `SELECT COUNT(*) FROM research_label_days`),
			count(t, db, `SELECT COUNT(*) FROM jev_decisions`), count(t, db, `SELECT COUNT(*) FROM market_snapshots`),
		}
	}
	before := untouched()
	r := featuremodel.NewRunner(repo.NewRepository(db)).WithClock(clock(t, "2026-10-12", "16:30")).WithPurgeBatches(10, 2)

	// 28 old predictions: 10 + 10 (a full second batch: More), then 8 and the 2 runs.
	res, err := r.Run(ctx, 30)
	if err != nil || !res.More || res.Purged != 20 {
		t.Fatalf("first Run = %+v, %v; want 20 purged and More", res, err)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM research_model_runs`); got != 4 {
		t.Errorf("%d runs after the first chunk, want 4 (runs wait for their predictions)", got)
	}
	res, err = r.Run(ctx, 30)
	if err != nil || res.More || res.Purged != 8+2 {
		t.Fatalf("second Run = %+v, %v; want 10 purged (8 predictions, 2 runs) and no More", res, err)
	}
	if runs, preds := count(t, db, `SELECT COUNT(*) FROM research_model_runs`), count(t, db, `SELECT COUNT(*) FROM research_model_predictions`); runs != 2 || preds != 6 {
		t.Errorf("%d runs and %d predictions left, want 2 and 6 (the cutoff day and the recent day)", runs, preds)
	}
	if count(t, db, `SELECT COUNT(*) FROM research_model_runs WHERE predict_day < '2026-09-12'`) != 0 {
		t.Error("an old run survived")
	}
	if after := untouched(); after != before {
		t.Errorf("tables outside the model's own changed: %v -> %v", before, after)
	}
	if res, err = r.Run(ctx, 30); err != nil || res.Purged != 0 || res.More {
		t.Errorf("third Run = %+v, %v; want nothing left", res, err)
	}
}
