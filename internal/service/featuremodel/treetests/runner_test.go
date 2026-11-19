package treetests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// seedDecisions plants two trader decisions of D with bars (return_5m +9 and -9) and
// a trader NONE that gets no prediction.
func seedDecisions(t *testing.T, db *sql.DB, d string) (long, short int64) {
	t.Helper()
	modeltest.SeedDay(t, db, d, modeltest.Day{Rows: 12, Seed: 60})
	at1, at2 := modeltest.At(t, d, "10:00", 3), modeltest.At(t, d, "10:00", 4)
	for at, r5 := range map[string]float64{at1.UTC().Format("2006-01-02T15:04:05.000000000Z"): 9, at2.UTC().Format("2006-01-02T15:04:05.000000000Z"): -9} {
		if _, err := db.Exec(`UPDATE market_snapshots SET return_5m = ? WHERE instrument_id = 1 AND timestamp = ?`, r5, at); err != nil {
			t.Fatal(err)
		}
	}
	long = modeltest.SeedDecision(t, db, at1, "trader", "LONG")
	short = modeltest.SeedDecision(t, db, at2, "trader", "SHORT")
	modeltest.SeedDecision(t, db, modeltest.At(t, d, "10:00", 5), "trader", "NONE")
	return long, short
}

func newRunner(db *sql.DB, now func() time.Time) *featuremodel.Runner {
	return featuremodel.NewRunner(repo.NewRepository(db)).WithSpecs(featuremodel.Specs()...).
		WithParams(testParams()).WithTreeParams(testTreeParams()).WithClock(now)
}

type storedRun struct {
	id                            int64
	version, algorithm, horizon   string
	features, hyper, coefficients string
	nTrain, nPredicted            int
}

func storedRuns(t *testing.T, db *sql.DB) []storedRun {
	t.Helper()
	rows, err := db.Query(`SELECT id, version, algorithm, label_horizon, features_json, hyperparams_json, coefficients_json, n_train, n_predicted FROM research_model_runs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []storedRun
	for rows.Next() {
		var r storedRun
		if err := rows.Scan(&r.id, &r.version, &r.algorithm, &r.horizon, &r.features, &r.hyper, &r.coefficients, &r.nTrain, &r.nPredicted); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The runner records, for the day D, one run per model (ridge and tree at 30m, 60m and
// close), each with the decisions' predictions keyed to the decision ids: algorithm,
// label horizon, features, hyperparameters and the fitted trees are stored, and the
// stored trees reproduce the stored predictions. A second pass writes nothing.
func TestRunnerRecordsTheTreeModelAndAllHorizons(t *testing.T) {
	ctx := context.Background()
	db, _, d := history(t)
	long, short := seedDecisions(t, db, d)
	r := newRunner(db, clock(t, "2026-10-12", "16:30"))

	var versions []string
	for i := 0; ; i++ {
		res, err := r.Run(ctx, 90)
		if err != nil {
			t.Fatal(err)
		}
		if res.Day != "" {
			versions = append(versions, res.Version)
		}
		if !res.More {
			break
		}
		if i > 10 {
			t.Fatal("Run kept saying More")
		}
	}
	want := []string{"rm-ridge1-30m-" + d, "rm-gbt1-30m-" + d, "rm-ridge1-60m-" + d, "rm-gbt1-60m-" + d, "rm-ridge1-close-" + d, "rm-gbt1-close-" + d}
	if !slices.Equal(versions, want) {
		t.Fatalf("recorded runs %v, want %v", versions, want)
	}
	runs := storedRuns(t, db)
	if len(runs) != 6 {
		t.Fatalf("%d run rows, want 6", len(runs))
	}
	for i, run := range runs {
		spec := featuremodel.Specs()[i]
		if run.version != want[i] || run.algorithm != spec.Algorithm || run.horizon != spec.Horizon || run.nTrain != 600 || run.nPredicted != 2 {
			t.Errorf("run %d = %s %s/%s n_train %d n_predicted %d, want %s %s/%s 600 2", i, run.version, run.algorithm, run.horizon, run.nTrain, run.nPredicted, want[i], spec.Algorithm, spec.Horizon)
		}
		var features []string
		var hp map[string]any
		for what, target := range map[string]any{run.features: &features, run.hyper: &hp} {
			if err := json.Unmarshal([]byte(what), target); err != nil {
				t.Fatalf("stored JSON %q is not valid: %v", what, err)
			}
		}
		if fmt.Sprint(features) != fmt.Sprint(repo.Features) || hp["label_horizon"] != spec.Horizon {
			t.Errorf("run %s records features %v, hyperparams %v", run.version, features, hp)
		}
		if spec.Algorithm == featuremodel.AlgorithmTree {
			tp := testTreeParams()
			if hp["trees"] != float64(tp.Trees) || hp["depth"] != float64(tp.Depth) || hp["learning_rate"] != tp.Rate || hp["min_leaf"] != float64(tp.MinLeaf) ||
				hp["bins"] != float64(tp.Bins) || hp["sample_buckets"] != float64(featuremodel.SampleBuckets) || hp["stride_rows"] != 1.0 || hp["randomness"] == nil {
				t.Errorf("tree run %s hyperparams = %v, want the settings of the fit", run.version, hp)
			}
			var model featuremodel.TreeModel
			if err := json.Unmarshal([]byte(run.coefficients), &model); err != nil || len(model.Long.Trees) != tp.Trees || len(model.Short.Trees) != tp.Trees || len(model.Mean) != len(repo.Features) {
				t.Errorf("run %s stored model: err %v, %d long trees", run.version, err, len(model.Long.Trees))
			}
			checkStoredPredictions(t, db, run.id, model, long, short)
		} else if hp["ridge_lambda"] != testParams().RidgeLambda {
			t.Errorf("ridge run %s hyperparams = %v", run.version, hp)
		}
	}

	// Predictions follow each horizon's own labels: at +9 / -9 the long side is +3x*scale
	// (the close horizon is flipped, so its pick is the other way round).
	for i, run := range runs {
		scale := modeltest.HorizonScale(featuremodel.Specs()[i].Horizon)
		var predDir string
		var l float64
		if err := db.QueryRow(`SELECT pred_long_net_bps, direction FROM research_model_predictions WHERE run_id = ? AND jev_decision_id = ?`, run.id, long).Scan(&l, &predDir); err != nil {
			t.Fatal(err)
		}
		if wantDir := map[bool]string{true: "LONG", false: "SHORT"}[scale > 0]; predDir != wantDir {
			t.Errorf("run %s picks %s at return_5m=+9, want %s", run.version, predDir, wantDir)
		}
		if r := l / (27 * scale); r < 0.5 || r > 1.5 {
			t.Errorf("run %s predicts long %.1f at return_5m=+9, want about %.1f", run.version, l, 27*scale)
		}
	}

	before := count(t, db, `SELECT COUNT(*) FROM research_model_runs`) + count(t, db, `SELECT COUNT(*) FROM research_model_predictions`)
	res, err := r.Run(ctx, 90)
	if err != nil || res.Day != "" || res.More {
		t.Fatalf("second pass = %+v, %v; want nothing to do", res, err)
	}
	if after := count(t, db, `SELECT COUNT(*) FROM research_model_runs`) + count(t, db, `SELECT COUNT(*) FROM research_model_predictions`); after != before {
		t.Errorf("rows %d -> %d on a second pass", before, after)
	}
}

// checkStoredPredictions recomputes the run's predictions from the stored tree model
// and the decisions' bars.
func checkStoredPredictions(t *testing.T, db *sql.DB, runID int64, model featuremodel.TreeModel, decisions ...int64) {
	t.Helper()
	for _, id := range decisions {
		raw := make([]sql.NullFloat64, len(repo.Features))
		dests := make([]any, len(raw))
		for i := range raw {
			dests[i] = &raw[i]
		}
		if err := db.QueryRow(`SELECT return_1m, return_5m, return_15m, return_30m, price_vs_vwap_bps, volume_ratio_5m, orderbook_imbalance, realized_vol_5m, spread_bps,
	market_return_5m, sector_return_5m, vwap_slope FROM market_snapshots m JOIN jev_decisions d ON d.instrument_id = m.instrument_id AND d.timestamp = m.timestamp WHERE d.id = ?`, id).Scan(dests...); err != nil {
			t.Fatal(err)
		}
		x := make([]float64, len(raw))
		for i, v := range raw {
			x[i] = math.NaN()
			if v.Valid {
				x[i] = v.Float64
			}
		}
		var pl, ps float64
		if err := db.QueryRow(`SELECT pred_long_net_bps, pred_short_net_bps FROM research_model_predictions WHERE run_id = ? AND jev_decision_id = ?`, runID, id).Scan(&pl, &ps); err != nil {
			t.Fatal(err)
		}
		if l, s := model.Predict(x); l != pl || s != ps {
			t.Errorf("run %d decision %d: stored trees give (%v, %v), stored prediction (%v, %v)", runID, id, l, s, pl, ps)
		}
	}
}

// The tree model's input handling on its own: a missing feature is imputed with the
// training mean, so an all-missing vector and one at the means predict the same.
func TestTreeModelImputesMissingFeaturesWithTheTrainingMean(t *testing.T) {
	db, _, d := history(t)
	m := mustTrain(t, db, featuremodel.Spec{Algorithm: featuremodel.AlgorithmTree, Horizon: "60m"}, d).Tree
	missing, atMean := make([]float64, len(repo.Features)), make([]float64, len(repo.Features))
	for i, f := range repo.Features {
		missing[i], atMean[i] = math.NaN(), m.Mean[f]
	}
	l1, s1 := m.Predict(missing)
	l2, s2 := m.Predict(atMean)
	if l1 != l2 || s1 != s2 {
		t.Errorf("missing -> %v/%v, at mean -> %v/%v; want equal", l1, s1, l2, s2)
	}
}

// With more rows than TreeMaxRows the tree model thins them by a fixed stride, so it
// holds a bounded number of rows, and records the stride.
func TestTreeModelThinsRowsAboveTheBound(t *testing.T) {
	db, _, d := history(t)
	tp := testTreeParams()
	tp.MaxRows = 250 // 600 rows -> every 3rd
	tr, err := featuremodel.TrainTree(context.Background(), repoOf(db), "30m", d, testParams(), tp)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Tree.StrideRows != 3 || tr.Tree.NLong != 200 || tr.Tree.NShort != 200 || tr.NTrain != 600 {
		t.Errorf("stride %d, rows %d/%d of %d; want stride 3 and 200/200 of 600", tr.Tree.StrideRows, tr.Tree.NLong, tr.Tree.NShort, tr.NTrain)
	}
}

// A model the runner does not know is an error before anything is trained or written.
func TestRunnerRejectsAnUnknownModel(t *testing.T) {
	db, _, d := history(t)
	seedDecisions(t, db, d)
	for _, spec := range []featuremodel.Spec{{Algorithm: "forest", Horizon: "30m"}, {Algorithm: "gbt", Horizon: "10m"}} {
		r := featuremodel.NewRunner(repo.NewRepository(db)).WithSpecs(spec).WithClock(clock(t, "2026-10-12", "16:30"))
		if _, err := r.Run(context.Background(), 90); err == nil {
			t.Errorf("%+v: Run succeeded, want an error", spec)
		}
	}
	if count(t, db, `SELECT COUNT(*) FROM research_model_runs`) != 0 {
		t.Error("a run was written for an unknown model")
	}
}
