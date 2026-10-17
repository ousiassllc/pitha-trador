// Package trainingflow_test holds the black-box tests of the meta-labeling
// training package (internal/service/metalabel/training). They only use its
// exported API and live in their own directory to keep that package within the
// linterly directory line budget.
package trainingflow_test

import (
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	servicemetalabel "github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// newStore returns a Store over a fresh, fully migrated database.
func newStore(t *testing.T) *servicemetalabel.Store {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return servicemetalabel.NewStore(metalabel.NewRepository(conn))
}

// syntheticSamples returns n samples whose win probability follows the first
// feature, so a model can learn it.
func syntheticSamples(rng *rand.Rand, n int) []servicemetalabel.Sample {
	out := make([]servicemetalabel.Sample, n)
	for i := range out {
		v := make([]float64, len(servicemetalabel.FeatureNames()))
		for j := range v {
			v[j] = rng.NormFloat64()
		}
		net := -20.0
		if rng.Float64() < 1/(1+math.Exp(-2*v[0])) {
			net = 30
		}
		out[i] = servicemetalabel.Sample{Vector: v, NetReturnBps: net}
	}
	return out
}
