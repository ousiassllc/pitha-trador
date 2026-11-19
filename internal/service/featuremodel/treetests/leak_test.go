package treetests

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// Walk-forward without leakage, for every model and horizon: nothing at or after the
// predicted day D changes the model that predicts D. Days D and D+1 with wild values
// and complete marks (all three horizons), a wild day before D that was never marked
// complete, and wild labels of the last training day whose exit reaches into D
// (purged) all leave the digest, the window and the fitted model as they were. The
// predictions on D's own bars are therefore the same too.
func TestTrainingNeverSeesTheFutureOrAnIncompleteDay(t *testing.T) {
	dbA, _, d := history(t)
	dbB, days, _ := history(t)
	wild := func(day string, mark bool, seed uint64) {
		modeltest.SeedDay(t, dbB, day, modeltest.Day{Rows: 60, Seed: seed, Mark: mark, Wild: true})
		modeltest.SeedHorizons(t, dbB, day)
	}
	wild(d, true, 50)
	wild("2026-10-12", true, 51)
	wild("2026-10-03", false, 52) // before D, not complete
	// Wild labels of the last training day that exit in D, at every horizon.
	last := days[len(days)-1]
	reach := modeltest.At(t, d, "00:00", 60)
	modeltest.SeedDay(t, dbB, last, modeltest.Day{Rows: 20, Seed: 53, Wild: true, ExitAt: reach, StartSec: 5000})
	modeltest.SeedHorizons(t, dbB, last)
	if _, err := dbB.Exec(`UPDATE research_horizon_labels SET exit_at = ? WHERE day = ? AND snapshot_at >= ?`,
		reach.UTC().Format("2006-01-02T15:04:05.000000000Z"), last, modeltest.At(t, last, "10:00", 5000).UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}

	for _, spec := range featuremodel.Specs() {
		a, b := mustTrain(t, dbA, spec, d), mustTrain(t, dbB, spec, d)
		ja, _ := json.Marshal(modelOf(a))
		jb, _ := json.Marshal(modelOf(b))
		if b.Digest != a.Digest || b.NTrain != a.NTrain || b.From != a.From || b.To != a.To || string(ja) != string(jb) {
			t.Errorf("%s/%s changed with data at or after D: window %+v vs %+v", spec.Algorithm, spec.Horizon, b, a)
		}
		if b.Purged != 20 {
			t.Errorf("%s/%s purged %d rows, want the 20 wild labels exiting in D", spec.Algorithm, spec.Horizon, b.Purged)
		}
	}
}

// The purge on its own, for every model: a label whose exit_at is not before the start
// of D is not a training row, however early its day is; one second earlier it is.
func TestRowsExitingAfterTheStartOfTheDayArePurgedAtEveryHorizon(t *testing.T) {
	startOfD := modeltest.At(t, "2026-10-11", "00:00", 0)
	for _, spec := range featuremodel.Specs() {
		db, _, d := history(t)
		before := mustTrain(t, db, spec, d)
		if before.Purged != 0 {
			t.Fatalf("%s/%s: Purged = %d on clean data, want 0", spec.Algorithm, spec.Horizon, before.Purged)
		}
		const layout = "2006-01-02T15:04:05.000000000Z"
		for _, q := range []struct {
			limit, offset int
			exit          time.Time
		}{{25, 0, startOfD}, {5, 25, startOfD.Add(-time.Second)}} {
			if _, err := db.Exec(`UPDATE research_horizon_labels SET exit_at = ? WHERE id IN (
	SELECT id FROM research_horizon_labels WHERE day = '2026-10-10' AND horizon = ? ORDER BY id LIMIT ? OFFSET ?)`, q.exit.UTC().Format(layout), spec.Horizon, q.limit, q.offset); err != nil {
				t.Fatal(err)
			}
		}
		after := mustTrain(t, db, spec, d)
		if after.Purged != 25 || after.NTrain != before.NTrain-25 || after.Digest == before.Digest {
			t.Errorf("%s/%s after moving exits: purged %d, rows %d (was %d); want 25 purged and 25 fewer rows", spec.Algorithm, spec.Horizon, after.Purged, after.NTrain, before.NTrain)
		}
	}
}

// A horizon is trained from its own labels and its own completion mark: a day whose
// 60m and close labels were not counted yet (the mark says 0) does not count as a
// training day for those horizons, though its 30m labels do; fewer complete days than
// the minimum is ErrInsufficient, not a fit on scraps.
func TestEachHorizonNeedsItsOwnCompleteDays(t *testing.T) {
	db, _, d := history(t)
	if _, err := db.Exec(`UPDATE research_label_days SET labeled_60m = 0, labeled_close = 0 WHERE day >= '2026-10-07'`); err != nil {
		t.Fatal(err)
	}
	for _, spec := range featuremodel.Specs() {
		_, err := train(t, db, spec, d)
		if spec.Horizon == "30m" {
			if err != nil {
				t.Errorf("30m: %v", err)
			}
			continue
		}
		if !errors.Is(err, featuremodel.ErrInsufficient) {
			t.Errorf("%s/%s with 2 complete days: err = %v, want ErrInsufficient", spec.Algorithm, spec.Horizon, err)
		}
	}
	// Too few rows for the tree's leaves is insufficient too (the ridge minimum already passed).
	small := modeltest.AllHorizonsDB(t, "2026-10-05", 6, 100)
	tp := testTreeParams()
	tp.MinLeaf = 400 // 600 rows cannot make two leaves of 400
	if _, err := featuremodel.TrainTree(t.Context(), repoOf(small), "60m", d, testParams(), tp); !errors.Is(err, featuremodel.ErrInsufficient) {
		t.Errorf("min leaf 400: err = %v, want ErrInsufficient", err)
	}
}
