package featuremodel_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// The training window is the most recent complete label days strictly before the
// predicted day, capped, oldest first; a day without its completion mark (or with no
// 30-minute rows) is not in it.
func TestTrainingDaysAreTheRecentCompleteDaysBeforeTheDay(t *testing.T) {
	db := modeltest.OpenDB(t)
	days := modeltest.Days(t, "2026-07-01", 70)
	for _, d := range days {
		if _, err := db.Exec(`INSERT INTO research_label_days (day, snapshots, labeled_30m, labeled_60m, labeled_close, completed_at) VALUES (?, 1, 1, 0, 0, 't')`, d); err != nil {
			t.Fatal(err)
		}
	}
	// 2026-08-10 has the mark but no 30-minute row; the mark of the predicted day and later exist too.
	if _, err := db.Exec(`UPDATE research_label_days SET labeled_30m = 0 WHERE day = '2026-08-10'`); err != nil {
		t.Fatal(err)
	}
	r := featuremodel.NewRepository(db)
	got, err := r.TrainingDays(context.Background(), "2026-09-05", 60)
	if err != nil {
		t.Fatal(err)
	}
	// Days 2026-07-01 .. 2026-09-04 are 66, minus 08-10: 65 candidates; the 60 most recent end at 09-04.
	if len(got) != 60 || got[len(got)-1] != "2026-09-04" || got[0] != "2026-07-06" {
		t.Errorf("window = %d days %v..%v, want 60 days 2026-07-06..2026-09-04", len(got), got[0], got[len(got)-1])
	}
	for i, d := range got {
		if d == "2026-08-10" || d >= "2026-09-05" || (i > 0 && got[i-1] >= d) {
			t.Fatalf("window holds %s at %d (unmarked, not before the day, or out of order)", d, i)
		}
	}
	if none, err := r.TrainingDays(context.Background(), "2026-07-01", 60); err != nil || len(none) != 0 {
		t.Errorf("days before the first mark = %v, %v; want none", none, err)
	}
}

// Only the 30-minute rows of sample_bucket below the limit, in (day, snapshot_id) order.
func TestScanTrainingOrderAndSample(t *testing.T) {
	db := modeltest.OpenDB(t)
	modeltest.SeedDay(t, db, "2026-10-06", modeltest.Day{Rows: 5, Seed: 1})
	modeltest.SeedDay(t, db, "2026-10-05", modeltest.Day{Rows: 5, Seed: 2})
	if _, err := db.Exec(`UPDATE research_horizon_labels SET sample_bucket = 50 WHERE id = 2`); err != nil { // not in the sample
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE research_horizon_labels SET horizon = '60m' WHERE id = 3`); err != nil { // not the 30m horizon
		t.Fatal(err)
	}
	var got []string
	purged, err := featuremodel.NewRepository(db).ScanTraining(context.Background(), []string{"2026-10-05", "2026-10-06"}, 10, "2026-10-07", func(r featuremodel.TrainRow) error {
		got = append(got, fmt.Sprintf("%s#%d", r.Day, r.SnapshotID))
		return nil
	})
	if err != nil || purged != 0 {
		t.Fatalf("ScanTraining = %d, %v", purged, err)
	}
	// Day 10-06 was seeded first (snapshot ids 1..5), 10-05 second (6..10); the order is by day, then id.
	if want := "[2026-10-05#6 2026-10-05#7 2026-10-05#8 2026-10-05#9 2026-10-05#10 2026-10-06#1 2026-10-06#4 2026-10-06#5]"; fmt.Sprint(got) != want {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if _, err := featuremodel.NewRepository(db).ScanTraining(context.Background(), []string{"2026-10-07"}, 10, "2026-10-07", func(featuremodel.TrainRow) error { return nil }); err == nil {
		t.Error("a training day that is not before the predicted day was accepted")
	}
}
