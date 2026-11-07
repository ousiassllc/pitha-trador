package metalabeltests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// funnelStepRows is the rows of one funnel step.
func funnelStepRows(t *testing.T, d readiness, step string) int64 {
	t.Helper()
	for _, s := range d.Funnel {
		if s.Step == step {
			return s.Rows
		}
	}
	t.Fatalf("no funnel step %q in %+v", step, d.Funnel)
	return 0
}

// Jev's model changed inside the 90-day lookback (jev-1.12.0 rows, then
// jev-1.13.0 rows to now). The nightly retraining fits, calibrates and
// evaluates on the jev-1.13.0 rows only, and metalabel_training_readiness
// counts the same rows, not the jev-1.12.0 ones.
func TestRetrainingAndReadinessUseOnlyTheLastAnsweringJevModel(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffModel, `"jev-1.13.0"`)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffConfirmedOn, `"2026-10-01"`)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffSource, `"vendor page"`)

	learnableHistory(t, w, now) // jev-1.13.0
	const olderRows = 300
	end := now.Add(-oldRowsAge).Add(-time.Duration(2*fitAndCalibration()) * 20 * time.Minute).Add(-time.Hour)
	for _, d := range learnable(olderRows, 50_000, end, "jev-1.12.0") {
		insertDecision(t, w, d)
	}
	wantRows := 2*fitAndCalibration() + oosBarRows()

	store := runNightly(t, w, now)
	if store.model == nil {
		t.Fatalf("the nightly retraining should store a model; skips = %v", store.skips)
	}
	m := *store.model
	if got := m.TrainSamples + m.OOSSamples; got > wantRows {
		t.Errorf("train %d + OOS %d samples exceed the %d jev-1.13.0 rows: jev-1.12.0 rows were used", m.TrainSamples, m.OOSSamples, wantRows)
	}
	if m.RejectReason == training.RejectLLMModelChanged {
		t.Errorf("reject_reason = %q for a single-model dataset", m.RejectReason)
	}

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	if d.JevModelID != "jev-1.13.0" {
		t.Errorf("jev_model_id = %q, want jev-1.13.0", d.JevModelID)
	}
	for _, step := range []string{"trader_decisions", "long_short", "label_v2", "net_return"} {
		if got := funnelStepRows(t, d, step); got != int64(wantRows) {
			t.Errorf("funnel %s = %d, want the %d jev-1.13.0 rows (not the %d jev-1.12.0 ones)", step, got, wantRows, olderRows)
		}
	}
	if d.Features.TrainingRows != wantRows {
		t.Errorf("features.training_rows = %d, want %d", d.Features.TrainingRows, wantRows)
	}
	if d.Split.Train.Rows != m.TrainSamples || d.Split.OOS.Rows != m.OOSSamples {
		t.Errorf("readiness train/oos rows = %d/%d, the model stored %d/%d", d.Split.Train.Rows, d.Split.OOS.Rows, m.TrainSamples, m.OOSSamples)
	}
}

// The 2026-11-04 case: the jev-1.13.0 history, then jev-1.14.0 answers inside
// the OOS window. The retraining no longer mixes them: it sees the jev-1.14.0
// rows only, which are too few, so it skips; readiness reports the same rows
// and the same skip. cutoffGate is untouched (TestJevModelsAgreesWithTheTrainersCutoffGate
// still pins its llm_model_changed rejection).
func TestMixedJevModelsInOOSWindowAreNotTrainedAcross(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffModel, `"jev-1.13.0"`)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffConfirmedOn, `"2026-10-01"`)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoffSource, `"vendor page"`)

	const newest = 40
	old := 2 * fitAndCalibration()
	for _, d := range learnable(old, 1, now.Add(-oldRowsAge), "jev-1.13.0") {
		insertDecision(t, w, d)
	}
	recent := learnable(oosBarRows(), 10_000, now.Add(-30*time.Minute), "jev-1.13.0")
	for i := range recent {
		if i >= len(recent)-newest {
			recent[i].model = "jev-1.14.0"
		}
		insertDecision(t, w, recent[i])
	}

	store := runNightly(t, w, now)
	if store.model != nil || len(store.skips) != 1 {
		t.Fatalf("with %d jev-1.14.0 rows only, the retraining should skip once and store no model: model=%v skips=%v", newest, store.model, store.skips)
	}
	skip := store.skips[0]
	if skip.UsableRows > newest {
		t.Errorf("usable rows = %d, more than the %d jev-1.14.0 rows: another model was read", skip.UsableRows, newest)
	}

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	if d.JevModelID != "jev-1.14.0" {
		t.Errorf("jev_model_id = %q, want jev-1.14.0", d.JevModelID)
	}
	if got := funnelStepRows(t, d, "net_return"); got != newest {
		t.Errorf("funnel net_return = %d, want the %d jev-1.14.0 rows", got, newest)
	}
	if d.Split.TrainReady || d.Split.TrainSkipCode == nil || *d.Split.TrainSkipCode != skip.ReasonCode || d.Split.Train.Rows != skip.UsableRows {
		t.Errorf("readiness split = %+v, want the Trainer's skip %q with %d rows", d.Split, skip.ReasonCode, skip.UsableRows)
	}
}
