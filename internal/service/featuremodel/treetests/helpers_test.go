// Package treetests tests the tree model and the 60m / close horizons of the research
// feature model (issue #1123): what they learn, that no future label reaches them,
// that they are reproducible, and that the nightly runner records them.
package treetests

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel"
	"github.com/ousiassllc/pitha-trador/internal/service/featuremodel/modeltest"
)

// testParams scale the production window to the fixtures (6 days of 100 bars: 600 rows
// per side); TestDefaultTreeParams pins the production ones.
func testParams() featuremodel.Params {
	p := featuremodel.DefaultParams()
	p.RidgeLambda, p.MinTrainRows = 20, 500
	return p
}

func testTreeParams() featuremodel.TreeParams {
	return featuremodel.TreeParams{Trees: 30, Depth: 3, Rate: 0.2, MinLeaf: 20, Bins: 16, MaxRows: 100000}
}

// history is 6 complete days with the labels of all three horizons and the day D after them.
func history(t *testing.T) (db *sql.DB, days []string, predict string) {
	t.Helper()
	return modeltest.AllHorizonsDB(t, "2026-10-05", 6, 100), modeltest.Days(t, "2026-10-05", 6), "2026-10-11"
}

// train fits the model of spec for the day on the repository over db.
func train(t *testing.T, db *sql.DB, spec featuremodel.Spec, day string) (*featuremodel.Trained, error) {
	t.Helper()
	src := repo.NewRepository(db)
	if spec.Algorithm == featuremodel.AlgorithmTree {
		return featuremodel.TrainTree(context.Background(), src, spec.Horizon, day, testParams(), testTreeParams())
	}
	return featuremodel.TrainRidge(context.Background(), src, spec.Horizon, day, testParams())
}

func mustTrain(t *testing.T, db *sql.DB, spec featuremodel.Spec, day string) *featuremodel.Trained {
	t.Helper()
	tr, err := train(t, db, spec, day)
	if err != nil {
		t.Fatalf("train %s/%s for %s: %v", spec.Algorithm, spec.Horizon, day, err)
	}
	return tr
}

func repoOf(db *sql.DB) *repo.Repository { return repo.NewRepository(db) }

// atReturn5m is a feature vector with only return_5m set (the rest missing).
func atReturn5m(v float64) []float64 {
	x := make([]float64, len(repo.Features))
	for i, f := range repo.Features {
		x[i] = math.NaN()
		if f == "return_5m" {
			x[i] = v
		}
	}
	return x
}

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
