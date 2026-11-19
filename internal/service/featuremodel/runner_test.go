package featuremodel_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// clock is a fixed JST clock at day hh:mm.
func clock(t *testing.T, day, hhmm string) func() time.Time {
	at := modeltest.At(t, day, hhmm, 0)
	return func() time.Time { return at }
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type decision struct {
	id int64
	at time.Time
}

// seedDecisions plants the decisions of day D: a LONG and a SHORT with bars (the bars
// are seeded unmarked, return_5m +9 / -9 planted), a trader NONE and a scout LONG
// (not evaluated), a trader LONG at a time with no bar, and a LONG on the next day.
func seedDecisions(t *testing.T, db *sql.DB, d string) (long, short, noBar decision) {
	t.Helper()
	modeltest.SeedDay(t, db, d, modeltest.Day{Rows: 12, Seed: 60})
	set := func(at time.Time, r5 float64) {
		if _, err := db.Exec(`UPDATE market_snapshots SET return_5m = ? WHERE instrument_id = 1 AND timestamp = ?`, r5, sqlutil.FormatTime(at)); err != nil {
			t.Fatal(err)
		}
	}
	long.at, short.at = modeltest.At(t, d, "10:00", 3), modeltest.At(t, d, "10:00", 4)
	set(long.at, 9)
	set(short.at, -9)
	long.id = modeltest.SeedDecision(t, db, long.at, "trader", "LONG")
	short.id = modeltest.SeedDecision(t, db, short.at, "trader", "SHORT")
	modeltest.SeedDecision(t, db, modeltest.At(t, d, "10:00", 5), "trader", "NONE")
	modeltest.SeedDecision(t, db, modeltest.At(t, d, "10:00", 6), "scout", "LONG")
	noBar.at = modeltest.At(t, d, "12:00", 0)
	noBar.id = modeltest.SeedDecision(t, db, noBar.at, "trader", "LONG")
	next := modeltest.Days(t, d, 2)[1]
	modeltest.SeedDay(t, db, next, modeltest.Day{Rows: 5, Seed: 61})
	modeltest.SeedDecision(t, db, modeltest.At(t, next, "10:00", 1), "trader", "LONG")
	return long, short, noBar
}

type predRow struct {
	runID, decisionID, instrument int64
	at, day, direction            string
	long, short                   float64
}

func predictions(t *testing.T, db *sql.DB) map[int64]predRow {
	t.Helper()
	rows, err := db.Query(`SELECT run_id, jev_decision_id, instrument_id, snapshot_at, day, direction, pred_long_net_bps, pred_short_net_bps FROM research_model_predictions`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]predRow{}
	for rows.Next() {
		var p predRow
		if err := rows.Scan(&p.runID, &p.decisionID, &p.instrument, &p.at, &p.day, &p.direction, &p.long, &p.short); err != nil {
			t.Fatal(err)
		}
		out[p.decisionID] = p
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The run of day D records, for each trader LONG/SHORT decision of D that has its own
// bar, the predictions keyed to the decision's id, instrument and timestamp; the
// direction follows the rule; a decision without a bar, a trader NONE, a scout
// decision and another day's decision get none. A second run of the same day writes
// nothing.
func TestRunnerRecordsPredictionsNextToTheDecisions(t *testing.T) {
	ctx := context.Background()
	db, _, d := baseHistory(t)
	long, short, noBar := seedDecisions(t, db, d)
	r := featuremodel.NewRunner(repo.NewRepository(db)).WithParams(testParams()).WithClock(clock(t, "2026-10-12", "16:30"))

	res, err := r.Run(ctx, 90)
	if err != nil || res.Day != d {
		t.Fatalf("Run = %+v, %v; want a run for %s", res, err, d)
	}
	if res.NPredict != 2 || res.NTrain != 600 {
		t.Errorf("Result = %+v, want 2 predictions on 600 training rows", res)
	}
	preds := predictions(t, db)
	if len(preds) != 2 {
		t.Fatalf("%d prediction rows, want 2 (LONG and SHORT with bars)", len(preds))
	}
	if _, ok := preds[noBar.id]; ok {
		t.Error("a decision without a bar got a prediction")
	}
	pl, ps := preds[long.id], preds[short.id]
	for _, c := range []struct {
		name string
		p    predRow
		d    decision
		want string
	}{{"long", pl, long, "LONG"}, {"short", ps, short, "SHORT"}} {
		if c.p.instrument != 1 || c.p.at != sqlutil.FormatTime(c.d.at) || c.p.day != d || c.p.direction != c.want {
			t.Errorf("%s prediction = %+v, want instrument 1 at %s on %s, direction %s", c.name, c.p, sqlutil.FormatTime(c.d.at), d, c.want)
		}
	}
	// return_5m = +9 / -9 against long = +3x, short = -2x: both signs follow the plant.
	if pl.long < 10 || pl.short > -5 || ps.long > -10 || ps.short < 5 {
		t.Errorf("predictions at +9 = (%v, %v), at -9 = (%v, %v); want long ~ +3x, short ~ -2x", pl.long, pl.short, ps.long, ps.short)
	}

	var version, features, hyper, coefs, digest string
	var nTrain, nPred int
	var runID int64
	if err := db.QueryRow(`SELECT id, version, features_json, hyperparams_json, coefficients_json, train_digest, n_train, n_predicted FROM research_model_runs`).
		Scan(&runID, &version, &features, &hyper, &coefs, &digest, &nTrain, &nPred); err != nil {
		t.Fatal(err)
	}
	if version != "rm-ridge1-30m-"+d || nTrain != 600 || nPred != 2 || len(digest) != 64 || pl.runID != runID {
		t.Errorf("run = %s n_train %d n_predicted %d digest %q run_id %d/%d", version, nTrain, nPred, digest, pl.runID, runID)
	}
	var fs []string
	var model featuremodel.Model
	var hp map[string]any
	for what, target := range map[string]any{features: &fs, coefs: &model, hyper: &hp} {
		if err := json.Unmarshal([]byte(what), target); err != nil {
			t.Fatalf("stored JSON %q is not valid: %v", what, err)
		}
	}
	if fmt.Sprint(fs) != fmt.Sprint(repo.Features) || len(model.Long.Coef) != len(repo.Features) || len(model.Std) != len(repo.Features) ||
		hp["ridge_lambda"] != testParams().RidgeLambda || hp["sample_buckets"] != float64(featuremodel.SampleBuckets) || hp["min_train_rows"] != float64(testParams().MinTrainRows) {
		t.Errorf("recorded JSON incomplete: features %v, model %+v, hyperparams %v", fs, model, hp)
	}
	// What was stored reproduces the stored predictions.
	x := make([]float64, len(repo.Features))
	rows, err := db.Query(`SELECT return_1m, return_5m, return_15m, return_30m, price_vs_vwap_bps, volume_ratio_5m, orderbook_imbalance, realized_vol_5m, spread_bps,
		market_return_5m, sector_return_5m, vwap_slope FROM market_snapshots WHERE instrument_id = 1 AND timestamp = ?`, sqlutil.FormatTime(long.at))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("no market_snapshots row for the prediction: %v", rows.Err())
	}
	raw := make([]sql.NullFloat64, len(x))
	dests := make([]any, len(x))
	for i := range raw {
		dests[i] = &raw[i]
	}
	if err := rows.Scan(dests...); err != nil {
		t.Fatal(err)
	}
	for i, v := range raw {
		x[i] = math.NaN()
		if v.Valid {
			x[i] = v.Float64
		}
	}
	if l, s := model.Predict(x); math.Abs(l-pl.long) > 1e-9 || math.Abs(s-pl.short) > 1e-9 {
		t.Errorf("stored coefficients give (%v, %v), stored prediction is (%v, %v)", l, s, pl.long, pl.short)
	}

	// Idempotent: the same day again writes nothing, and the next day's decision is
	// not predicted by this run.
	runs, predsN := count(t, db, `SELECT COUNT(*) FROM research_model_runs`), count(t, db, `SELECT COUNT(*) FROM research_model_predictions`)
	again, err := r.Run(ctx, 90)
	if err != nil {
		t.Fatal(err)
	}
	if again.Day == d || count(t, db, `SELECT COUNT(*) FROM research_model_runs WHERE predict_day = ?`, d) != 1 {
		t.Errorf("second Run = %+v; want no second run for %s", again, d)
	}
	if got := count(t, db, `SELECT COUNT(*) FROM research_model_predictions WHERE run_id = ?`, runID); got != predsN || runs != 1 {
		t.Errorf("run %d has %d predictions after a re-run, want %d", runID, got, predsN)
	}
}

// The store itself ignores a run whose version exists: nothing of the second call
// (neither the run nor its predictions) is written.
func TestInsertRunIsIdempotent(t *testing.T) {
	db, _, d := baseHistory(t)
	long, _, _ := seedDecisions(t, db, d)
	store := repo.NewRepository(db)
	run := repo.Run{Version: featuremodel.Version(d), Algorithm: "ridge", LabelHorizon: "30m", FeaturesJSON: "[]", HyperparamsJSON: "{}", CoefficientsJSON: "{}",
		TrainFrom: "2026-10-05", TrainTo: "2026-10-10", PredictDay: d, NTrain: 1, TrainDigest: "x"}
	p := repo.Prediction{DecisionID: long.id, InstrumentID: 1, At: long.at, Day: d, Long: 1, Short: 2, Direction: "SHORT"}
	now := modeltest.At(t, d, "16:30", 0)
	if ok, err := store.InsertRun(context.Background(), run, []repo.Prediction{p}, now); err != nil || !ok {
		t.Fatalf("first InsertRun = %v, %v", ok, err)
	}
	p.Long = 99
	if ok, err := store.InsertRun(context.Background(), run, []repo.Prediction{p}, now); err != nil || ok {
		t.Fatalf("second InsertRun = %v, %v; want ignored", ok, err)
	}
	var stored float64
	if err := db.QueryRow(`SELECT pred_long_net_bps FROM research_model_predictions`).Scan(&stored); err != nil || stored != 1 || count(t, db, `SELECT COUNT(*) FROM research_model_runs`) != 1 {
		t.Errorf("stored long prediction %v (err %v), want the first call's 1 and one run", stored, err)
	}
}

// A day with too little history writes no run and no prediction; the next run
// (once there is history) still trains it.
func TestRunnerWritesNothingWithoutEnoughHistory(t *testing.T) {
	ctx := context.Background()
	db := modeltest.HistoryDB(t, "2026-10-05", 3, 20)
	seedDecisions(t, db, "2026-10-11")
	r := featuremodel.NewRunner(repo.NewRepository(db)).WithParams(testParams()).WithClock(clock(t, "2026-10-12", "16:30"))
	res, err := r.Run(ctx, 90)
	if err != nil || res.Day != "" || len(res.Skipped) == 0 {
		t.Fatalf("Run = %+v, %v; want nothing trained and the day skipped", res, err)
	}
	if count(t, db, `SELECT COUNT(*) FROM research_model_runs`)+count(t, db, `SELECT COUNT(*) FROM research_model_predictions`) != 0 {
		t.Error("rows were written without enough history")
	}
}

// One day per Run, oldest first, only within the last LookbackDays days and only days
// with trader LONG/SHORT decisions; More says days are left.
func TestRunnerTakesOneDayPerRunOldestFirst(t *testing.T) {
	ctx := context.Background()
	db := modeltest.HistoryDB(t, "2026-09-20", 6, 20)
	for _, day := range []string{"2026-10-01", "2026-10-08", "2026-10-09", "2026-10-11"} {
		modeltest.SeedDay(t, db, day, modeltest.Day{Rows: 4, Seed: 70})
		modeltest.SeedDecision(t, db, modeltest.At(t, day, "10:00", 1), "trader", "LONG")
	}
	modeltest.SeedDay(t, db, "2026-10-10", modeltest.Day{Rows: 4, Seed: 71}) // no decision: no run
	p := testParams()
	p.MinTrainRows = 100 // 6 days of 20 bars
	r := featuremodel.NewRunner(repo.NewRepository(db)).WithParams(p).WithClock(clock(t, "2026-10-12", "16:30"))
	var got []string
	for i := 0; i < 6; i++ {
		res, err := r.Run(ctx, 90)
		if err != nil {
			t.Fatal(err)
		}
		if res.Day != "" {
			got = append(got, res.Day)
		}
		if res.Day != "" && res.More != (len(got) < 3) {
			t.Errorf("after %v More = %v", got, res.More)
		}
		if !res.More {
			break
		}
	}
	// 2026-10-01 is older than 7 days before 10-12 (10-05); 10-10 has no decision.
	if fmt.Sprint(got) != "[2026-10-08 2026-10-09 2026-10-11]" {
		t.Errorf("trained days %v, want [2026-10-08 2026-10-09 2026-10-11]", got)
	}
}
