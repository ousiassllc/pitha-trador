package trainingflow_test

import (
	"context"
	"log/slog"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// documentedFalseAdoptionBound is the bound FR-META-7 documents for the share of
// edge-free datasets whose model is adopted.
const documentedFalseAdoptionBound = 0.05

// adoptionRate trains and judges one model per dataset under the default bar and
// returns the share adopted and the reject reasons seen.
func adoptionRate(t *testing.T, runs int, dataset func(run int) []repo.TrainingRow) (float64, map[string]int) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	adopted, reasons := 0, map[string]int{}
	for run := range runs {
		tr := newTrainer(&fakeSource{rows: dataset(run)}, &memStore{}, settingsOf(nil),
			training.WithAcceptance(training.DefaultAcceptanceConfig()), training.WithChangeLimits(changeguard.DefaultLimits()))
		m, err := tr.Train(context.Background())
		if err != nil {
			t.Fatalf("run %d: Train: %v", run, err)
		}
		if m.Status == repo.ModelActive {
			adopted++
		}
		reasons[m.RejectReason]++
	}
	return float64(adopted) / float64(runs), reasons
}

// On pure-noise data (no edge, outcomes unrelated to every feature, with a
// strong shared day effect) the default bar adopts the freshly trained model in
// at most 5% of the repeated train-and-judge runs. 3 training days + the 5
// trading days of the OOS window, 260 decisions a day. Seeded, so the result
// never varies.
func TestAdoption_FalseAdoptionRateOnNoiseIsWithinTheBound(t *testing.T) {
	rate, reasons := adoptionRate(t, 60, func(run int) []repo.TrainingRow {
		return noiseRows(8, 260, 0.12, 0, uint64(5000+run))
	})
	t.Logf("false adoption rate %.1f%% (reasons %v)", 100*rate, reasons)
	if rate > documentedFalseAdoptionBound {
		t.Errorf("false adoption rate = %.1f%%, want <= %.0f%%; reasons %v", 100*rate, 100*documentedFalseAdoptionBound, reasons)
	}
	if reasons[training.RejectEV]+reasons[training.RejectTakes]+reasons[training.RejectAUC] == 0 {
		t.Errorf("no run reached the confidence-interval criteria: %v", reasons)
	}
}

// The bar is attainable: with a real edge the same pipeline adopts the model.
func TestAdoption_RealEdgeIsAdoptedUnderTheDefaultBar(t *testing.T) {
	rate, reasons := adoptionRate(t, 8, func(run int) []repo.TrainingRow {
		return noiseRows(8, 360, 0.02, 0.25, uint64(9000+run))
	})
	if rate < 0.75 {
		t.Errorf("adoption rate with a real edge = %.0f%%, want >= 75%%; reasons %v", 100*rate, reasons)
	}
}
