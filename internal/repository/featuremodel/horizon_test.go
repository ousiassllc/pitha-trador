package featuremodel_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// Each horizon reads its own label rows and its own completion count: the 60m and
// close rows carry their own nets, and a day with no row of the horizon is not one of
// its training days. A horizon the table does not hold is an error, never a query.
func TestTrainingReadsEachHorizonsOwnLabelsAndDays(t *testing.T) {
	db := modeltest.OpenDB(t)
	modeltest.SeedDay(t, db, "2026-10-05", modeltest.Day{Rows: 3, Seed: 1, Mark: true})
	modeltest.SeedDay(t, db, "2026-10-06", modeltest.Day{Rows: 3, Seed: 2, Mark: true})
	modeltest.SeedHorizons(t, db, "2026-10-05") // 2026-10-06 has 30m labels only
	if _, err := db.Exec(`UPDATE research_label_days SET labeled_60m = 0, labeled_close = 0 WHERE day = '2026-10-06'`); err != nil {
		t.Fatal(err)
	}
	r := featuremodel.NewRepository(db)
	for horizon, want := range map[string]string{"30m": "[2026-10-05 2026-10-06]", "60m": "[2026-10-05]", "close": "[2026-10-05]"} {
		days, err := r.TrainingDays(context.Background(), horizon, "2026-10-07", 60)
		if err != nil || fmt.Sprint(days) != want {
			t.Errorf("TrainingDays(%s) = %v, %v; want %s", horizon, days, err, want)
		}
	}
	var net30, net60, netClose []float64
	for horizon, into := range map[string]*[]float64{"30m": &net30, "60m": &net60, "close": &netClose} {
		purged, err := r.ScanTraining(context.Background(), horizon, []string{"2026-10-05"}, 10, "2026-10-07", func(row featuremodel.TrainRow) error {
			*into = append(*into, row.LongNet)
			return nil
		})
		if err != nil || purged != 0 || len(*into) != 3 {
			t.Fatalf("ScanTraining(%s) = %d, %v with %d rows", horizon, purged, err, len(*into))
		}
	}
	for i := range net30 {
		if net60[i] != 2*net30[i] || netClose[i] != -net30[i] {
			t.Errorf("row %d: 30m %v, 60m %v, close %v; want 60m = 2x and close = -1x of the 30m net", i, net30[i], net60[i], netClose[i])
		}
	}
	if _, err := r.TrainingDays(context.Background(), "10m", "2026-10-07", 60); err == nil {
		t.Error("TrainingDays accepted the 10m horizon")
	}
	if _, err := r.ScanTraining(context.Background(), "2h", []string{"2026-10-05"}, 10, "2026-10-07", func(featuremodel.TrainRow) error { return nil }); err == nil {
		t.Error("ScanTraining accepted an unknown horizon")
	}
}
