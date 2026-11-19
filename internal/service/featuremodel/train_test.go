package featuremodel_test

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// testParams are the production hyperparameters scaled to the small fixtures: the
// minimum rows and the penalty shrink with the data (the production values, 2000 rows
// and lambda 1000, are pinned by TestDefaultParams).
func testParams() featuremodel.Params {
	p := featuremodel.DefaultParams()
	p.RidgeLambda, p.MinTrainRows = 20, 500
	return p
}

// baseHistory is 6 marked days of 100 bars (600 rows per side, one seeded database
// shared by every test, each getting its own copy) and the day D after them.
func baseHistory(t *testing.T) (db *sql.DB, days []string, predict string) {
	t.Helper()
	return modeltest.HistoryDB(t, "2026-10-05", 6, 100), modeltest.Days(t, "2026-10-05", 6), "2026-10-11"
}

func mustTrain(t *testing.T, db *sql.DB, day string) *featuremodel.Trained {
	t.Helper()
	tr, err := featuremodel.Train(context.Background(), repo.NewRepository(db), day, testParams())
	if err != nil {
		t.Fatalf("Train(%s): %v", day, err)
	}
	return tr
}

// The model learns what the data says: with long_net = +3 * return_5m and
// short_net = -2 * return_5m, the recorded coefficient and the prediction have the
// right sign and (ridge shrinks a little) about the right size, and the noise
// features get nearly nothing.
func TestTrainLearnsThePlantedSignal(t *testing.T) {
	db, _, d := baseHistory(t)
	tr := mustTrain(t, db, d)

	std := tr.Model.Std["return_5m"]
	if want := modeltest.Return5mRange / math.Sqrt(3); math.Abs(std-want) > 0.5 {
		t.Fatalf("std(return_5m) = %v, want about %v", std, want)
	}
	// coefficient per standardized unit = slope * std, shrunk a little by the ridge penalty
	for _, c := range []struct {
		name  string
		got   float64
		slope float64
	}{{"long", tr.Model.Long.Coef["return_5m"], modeltest.LongSlope}, {"short", tr.Model.Short.Coef["return_5m"], modeltest.ShortSlope}} {
		want := c.slope * std
		if r := c.got / want; r < 0.8 || r > 1.2 {
			t.Errorf("%s coefficient %v is %.2f of the planted %v, want 0.8..1.2", c.name, c.got, r, want)
		}
	}
	for _, name := range []string{"return_1m", "volume_ratio_5m", "orderbook_imbalance", "market_return_5m"} {
		if c := tr.Model.Long.Coef[name]; math.Abs(c) > 0.5 {
			t.Errorf("noise feature %s got a long coefficient %v", name, c)
		}
	}
	x := make([]float64, len(repo.Features))
	for i := range x {
		x[i] = math.NaN()
	}
	for i, f := range repo.Features {
		if f == "return_5m" {
			x[i] = 8
		}
	}
	long8, short8 := tr.Model.Predict(x)
	x[1] = -8
	longM8, shortM8 := tr.Model.Predict(x)
	if long8 < 18 || longM8 > -18 || long8 > 3*8*1.1 {
		t.Errorf("long prediction at return_5m=+-8 is %v / %v, want about +-(18..26)", long8, longM8)
	}
	if short8 > -12 || shortM8 < 12 {
		t.Errorf("short prediction at return_5m=+-8 is %v / %v, want about -+(12..18)", short8, shortM8)
	}
	if tr.NLong != 600 || tr.NShort != 600 || tr.NTrain != 600 || tr.Days != 6 || tr.From != "2026-10-05" || tr.To != "2026-10-10" {
		t.Errorf("window = %+v, want 6 days 2026-10-05..10 and 600 rows", tr)
	}
}

// A missing feature is imputed with the training mean: all-missing input predicts the
// intercept, and so does an input at the means.
func TestMissingFeaturesAreImputedWithTheTrainingMean(t *testing.T) {
	db, _, d := baseHistory(t)
	m := mustTrain(t, db, d).Model
	missing, atMean := make([]float64, len(repo.Features)), make([]float64, len(repo.Features))
	for i, f := range repo.Features {
		missing[i], atMean[i] = math.NaN(), m.Mean[f]
	}
	l1, s1 := m.Predict(missing)
	l2, s2 := m.Predict(atMean)
	if l1 != m.Long.Intercept || s1 != m.Short.Intercept || math.Abs(l1-l2) > 1e-9 || math.Abs(s1-s2) > 1e-9 {
		t.Errorf("missing -> %v/%v, at mean -> %v/%v, intercepts %v/%v; want all equal", l1, s1, l2, s2, m.Long.Intercept, m.Short.Intercept)
	}
}

// Same data and same constants give bit-identical coefficients, digest and window,
// in the same database and in a second one built the same way.
func TestTrainIsDeterministic(t *testing.T) {
	db, _, d := baseHistory(t)
	a, b := mustTrain(t, db, d), mustTrain(t, db, d)
	db2, _, _ := baseHistory(t) // a second database with the same content
	c := mustTrain(t, db2, d)
	for i, o := range []*featuremodel.Trained{b, c} {
		if o.Digest != a.Digest || len(a.Digest) != 64 {
			t.Errorf("digest %d = %q, want %q", i, o.Digest, a.Digest)
		}
		if o.Model.Long.Intercept != a.Model.Long.Intercept || o.Model.Short.Intercept != a.Model.Short.Intercept {
			t.Errorf("intercepts differ in repeat %d", i)
		}
		for _, f := range repo.Features {
			if o.Model.Long.Coef[f] != a.Model.Long.Coef[f] || o.Model.Short.Coef[f] != a.Model.Short.Coef[f] || o.Model.Mean[f] != a.Model.Mean[f] || o.Model.Std[f] != a.Model.Std[f] {
				t.Errorf("repeat %d differs on feature %s", i, f)
			}
		}
	}
	// And different data gives a different digest.
	modeltest.SeedDay(t, db2, "2026-10-04", modeltest.Day{Rows: 10, Seed: 99, Mark: true})
	if mustTrain(t, db2, d).Digest == a.Digest {
		t.Error("digest did not change with the training rows")
	}
}

// Walk-forward without leakage: nothing at or after the predicted day D changes the
// model that predicts D. Days D and D+1 with wild values and complete marks, a wild
// day before D that was never marked complete, wild feature values on D's own bar,
// and labels of the last training day whose exit reaches into D (purged) all leave the
// coefficients, the digest and the window as they were.
func TestTrainNeverSeesTheFutureOrAnIncompleteDay(t *testing.T) {
	dbA, _, d := baseHistory(t)
	base := mustTrain(t, dbA, d)

	dbB, days, _ := baseHistory(t)
	modeltest.SeedDay(t, dbB, d, modeltest.Day{Rows: 60, Seed: 50, Mark: true, Wild: true})
	modeltest.SeedDay(t, dbB, "2026-10-12", modeltest.Day{Rows: 60, Seed: 51, Mark: true, Wild: true})
	modeltest.SeedDay(t, dbB, "2026-10-03", modeltest.Day{Rows: 60, Seed: 52, Mark: false, Wild: true}) // before D, not complete
	// Labels of the last training day (a day before D) that exit in D: purged.
	reach := modeltest.At(t, d, "00:00", 60)
	modeltest.SeedDay(t, dbB, days[len(days)-1], modeltest.Day{Rows: 20, Seed: 53, Wild: true, ExitAt: reach, StartSec: 5000})

	got := mustTrain(t, dbB, d)
	if got.Digest != base.Digest || got.NTrain != base.NTrain || got.From != base.From || got.To != base.To {
		t.Errorf("window changed: %+v vs %+v", got, base)
	}
	for _, f := range repo.Features {
		if got.Model.Long.Coef[f] != base.Model.Long.Coef[f] || got.Model.Short.Coef[f] != base.Model.Short.Coef[f] {
			t.Errorf("coefficient of %s changed with data at or after D", f)
		}
	}
	if got.Model.Long.Intercept != base.Model.Long.Intercept {
		t.Error("intercept changed with data at or after D")
	}
}

// The purge on its own: a label whose exit_at is not before the start of D is not a
// training row, however early its day is, and is counted as purged.
func TestRowsExitingAfterTheStartOfTheDayArePurged(t *testing.T) {
	db, _, d := baseHistory(t)
	before := mustTrain(t, db, d)
	if before.Purged != 0 {
		t.Fatalf("Purged = %d on clean data, want 0", before.Purged)
	}
	startOfD := modeltest.At(t, d, "00:00", 0)
	// Move the exit of 25 labels of the last training day to exactly the start of D,
	// and of the next 5 to one second before it: the first 25 are purged, the 5 stay.
	const layout = "2006-01-02T15:04:05.000000000Z"
	for _, q := range []struct {
		limit, offset int
		exit          time.Time
	}{{25, 0, startOfD}, {5, 25, startOfD.Add(-time.Second)}} {
		if _, err := db.Exec(`UPDATE research_horizon_labels SET exit_at = ? WHERE id IN (
	SELECT id FROM research_horizon_labels WHERE day = '2026-10-10' ORDER BY id LIMIT ? OFFSET ?)`, q.exit.UTC().Format(layout), q.limit, q.offset); err != nil {
			t.Fatal(err)
		}
	}
	after := mustTrain(t, db, d)
	if after.Purged != 25 || after.NTrain != before.NTrain-25 || after.Digest == before.Digest {
		t.Errorf("after moving exits: purged %d, rows %d (was %d); want 25 purged and 25 fewer rows", after.Purged, after.NTrain, before.NTrain)
	}
}

// Too little history: fewer than MinTrainDays complete days, or fewer than
// MinTrainRows rows per side, is ErrInsufficient (no model), not a fit on scraps.
func TestTrainNeedsEnoughHistory(t *testing.T) {
	train := func(db *sql.DB, day string) error {
		_, err := featuremodel.Train(context.Background(), repo.NewRepository(db), day, testParams())
		return err
	}
	if err := train(modeltest.HistoryDB(t, "2026-10-05", featuremodel.MinTrainDays-1, 200), "2026-10-11"); !errors.Is(err, featuremodel.ErrInsufficient) {
		t.Errorf("4 days: err = %v, want ErrInsufficient", err)
	}
	if err := train(modeltest.HistoryDB(t, "2026-10-05", 6, 60), "2026-10-11"); !errors.Is(err, featuremodel.ErrInsufficient) { // 360 rows < 500
		t.Errorf("360 rows: err = %v, want ErrInsufficient", err)
	}
	// Days are counted before D only: the 6 days predict D=10-11 but not 10-08 (3 days before).
	db, _, _ := baseHistory(t)
	if err := train(db, "2026-10-08"); !errors.Is(err, featuremodel.ErrInsufficient) {
		t.Errorf("only 3 days before the predicted day: err = %v, want ErrInsufficient", err)
	}
}

// The production hyperparameters are the ones the docs and the recorded runs state.
func TestDefaultParams(t *testing.T) {
	want := featuremodel.Params{RidgeLambda: 1000, SampleBuckets: 10, MinTrainRows: 2000, MinTrainDays: 5, MaxTrainDays: 60}
	if got := featuremodel.DefaultParams(); got != want {
		t.Errorf("DefaultParams = %+v, want %+v", got, want)
	}
}

func TestDirectionRule(t *testing.T) {
	for _, c := range []struct {
		long, short float64
		want        string
	}{
		{3, 1, "LONG"}, {3, -1, "LONG"}, {1, 3, "SHORT"}, {-1, 3, "SHORT"},
		{-1, -2, "NONE"}, {0, 0, "NONE"}, {2, 2, "NONE"}, {0, -1, "NONE"}, {-2, 0, "NONE"},
	} {
		if got := featuremodel.Direction(c.long, c.short); got != c.want {
			t.Errorf("Direction(%v, %v) = %s, want %s", c.long, c.short, got, c.want)
		}
	}
}
