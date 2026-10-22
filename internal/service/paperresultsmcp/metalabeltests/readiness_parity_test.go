package metalabeltests

import (
	"testing"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Too little history: the nightly retraining skips, and the tool reports the
// same reason code, the same usable and required counts and the same OOS rows
// the real Trainer split the same rows into.
func TestReadinessMatchesTheNightlyRetrainingWhenItSkips(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	const recent = 50 // far below the OOS rows the bar asks for
	old := fitAndCalibration() - 50
	history(t, w, now, old, recent)

	store := runNightly(t, w, now)
	if store.model != nil || len(store.skips) != 1 {
		t.Fatalf("the nightly retraining should skip once and store no model: model=%v skips=%v", store.model, store.skips)
	}
	skip := store.skips[0]

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	if !d.MatchesNightlyRetrain || d.LookbackDays != 90 {
		t.Fatalf("matches_nightly_retrain = %v, lookback_days = %d", d.MatchesNightlyRetrain, d.LookbackDays)
	}
	if d.Split.TrainReady || d.Split.TrainSkipCode == nil || *d.Split.TrainSkipCode != skip.ReasonCode {
		t.Errorf("train skip code = %v, want the Trainer's %q", d.Split.TrainSkipCode, skip.ReasonCode)
	}
	if d.Split.Train.Rows != skip.UsableRows || d.Split.Train.RequiredRows != skip.RequiredRows {
		t.Errorf("train rows = %d of %d, the Trainer recorded %d of %d", d.Split.Train.Rows, d.Split.Train.RequiredRows, skip.UsableRows, skip.RequiredRows)
	}
	if d.Split.Train.FitRows != nil || d.Split.Train.CalibrationRows != nil {
		t.Errorf("fit/calibration rows are null while the split fails, got %v / %v", d.Split.Train.FitRows, d.Split.Train.CalibrationRows)
	}
	if d.Split.OOS.Rows != recent || d.Split.OOS.Ready || d.Split.OOS.RequiredRows != oosBarRows() {
		t.Errorf("oos = %+v, want the %d recent rows of %d and not ready", d.Split.OOS, recent, oosBarRows())
	}
	// The usable rows the skip names are exactly the old rows (before the OOS window).
	if skip.UsableRows != old {
		t.Errorf("fixture drifted: Trainer usable rows = %d, want %d", skip.UsableRows, old)
	}
}

// Enough history: the Trainer learns a model, and the tool's fit + calibration
// rows, OOS rows and OOS start are the ones the model was stored with.
func TestReadinessMatchesTheNightlyRetrainingWhenItLearns(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	learnableHistory(t, w, now)

	store := runNightly(t, w, now)
	if store.model == nil {
		t.Fatalf("the nightly retraining should store a model; skips = %v", store.skips)
	}
	m := *store.model

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	if !d.Split.TrainReady || d.Split.TrainSkipCode != nil {
		t.Fatalf("train_ready = %v, code %v; the Trainer learned a model", d.Split.TrainReady, d.Split.TrainSkipCode)
	}
	if d.Split.Train.Rows != m.TrainSamples || d.Split.Train.FitRows == nil || d.Split.Train.CalibrationRows == nil ||
		*d.Split.Train.FitRows+*d.Split.Train.CalibrationRows != m.TrainSamples {
		t.Errorf("train rows = %d (fit %v + calibration %v), the model stored %d", d.Split.Train.Rows, d.Split.Train.FitRows, d.Split.Train.CalibrationRows, m.TrainSamples)
	}
	if d.Split.OOS.Rows != m.OOSSamples {
		t.Errorf("oos rows = %d, the model stored %d", d.Split.OOS.Rows, m.OOSSamples)
	}
	if d.Split.OOS.Days != m.OOSDays {
		t.Errorf("oos days = %d, the model stored %d", d.Split.OOS.Days, m.OOSDays)
	}
	if want := m.OOSStart.UTC().Format(time.RFC3339); d.Split.OOSStart != want {
		t.Errorf("oos_start = %s, the model stored %s", d.Split.OOSStart, want)
	}
	// The Trainer ran against a capturing store, so no model or skip row was
	// written to the database the tool reads.
	if d.LastRetrain.LastRun != "none" {
		t.Errorf("last_run = %q, want none: nothing was written to the database", d.LastRetrain.LastRun)
	}
}

// The required counts are read from the retraining's configuration, not typed
// into the tool: whatever DefaultWalkForwardConfig and DefaultAcceptanceConfig
// hold (a change in issue #862 included) is what the tool reports.
func TestReadinessRequiredCountsComeFromTheRetrainingConfig(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	history(t, w, now, 10, 5)

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	walk, bar := training.DefaultWalkForwardConfig(), training.DefaultAcceptanceConfig()
	if d.Split.Train.RequiredRows != walk.MinFit+walk.MinCalibration ||
		d.Split.Train.RequiredFitRows != walk.MinFit || d.Split.Train.RequiredCalibrationRows != walk.MinCalibration {
		t.Errorf("train requirement = %+v, want fit %d + calibration %d", d.Split.Train, walk.MinFit, walk.MinCalibration)
	}
	if d.Split.OOS.RequiredRows != bar.MinOOSSamples || d.Split.OOS.RequiredDays != bar.MinOOSDays || d.Split.OOS.RequiredTake != bar.MinTakes {
		t.Errorf("oos requirement = %+v, want samples %d, days %d, takes %d", d.Split.OOS, bar.MinOOSSamples, bar.MinOOSDays, bar.MinTakes)
	}
}

// The feature gaps and dropped_incomplete are the Trainer's own BuildRows
// result on the rows it reads, and each decision that fails is counted under
// every feature it lacks.
func TestReadinessFeatureGapsMatchBuildRows(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	rows := learnable(60, 1, now.Add(-30*time.Minute), "jev-1.13.0")
	rows[0].ofi, rows[0].micro = nil, nil                  // no order flow: 4 features missing
	rows[1].state = `{"return_1m":0.0004}`                 // no spread_bps
	rows[2].state = `not json`                             // unreadable state: spread_bps and return_1m
	rows[3].response = `{"direction":"LONG"}`              // an unscorable answer: jev_score
	rows[4].state = `{"spread_bps":3.2}`                   // no return_1m
	rows[5].response, rows[5].state = `garbage`, `{"x":1}` // jev_score, spread_bps and return_1m
	for _, d := range rows {
		insertDecision(t, w, d)
	}
	stored, err := repo.NewRepository(w).ListTrainingRows(t.Context(), now.AddDate(0, 0, -90), now)
	if err != nil {
		t.Fatal(err)
	}
	built, dropped := training.BuildRows(stored)

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)
	if d.Features.TrainingRows != len(stored) || d.Features.UsableRows != len(built) || d.Features.DroppedIncomplete != dropped {
		t.Fatalf("features = rows %d usable %d dropped %d; the Trainer read %d, built %d, dropped %d",
			d.Features.TrainingRows, d.Features.UsableRows, d.Features.DroppedIncomplete, len(stored), len(built), dropped)
	}
	if dropped != 6 {
		t.Fatalf("fixture drifted: BuildRows dropped %d, want 6", dropped)
	}
	got := map[string]int{}
	for _, m := range d.Features.Missing {
		got[m.Feature] = m.MissingRows
	}
	want := map[string]int{
		"ofi_short": 1, "ofi_mid": 1, "ofi_long": 1, "microprice_dev_bps": 1,
		"spread_bps": 3, "return_1m": 3, "jev_score": 2, "tdnet_score": 0,
	}
	if len(got) != len(want) {
		t.Errorf("missing features = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("missing %s = %d, want %d", k, got[k], v)
		}
	}
	if len(d.Funnel) != 5 || d.Funnel[4].Step != metalabeldiag.StepFeaturesComplete || d.Funnel[4].Rows != int64(len(built)) {
		t.Errorf("funnel = %+v, want the last step to leave the %d usable rows", d.Funnel, len(built))
	}
}
