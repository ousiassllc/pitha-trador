package metalabeltests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/metalabeldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// readiness is the tool's data as the test decodes it.
type readiness struct {
	AsOf                  string `json:"as_of"`
	RowsFrom              string `json:"rows_from"`
	RowsTo                string `json:"rows_to"`
	LookbackDays          int    `json:"lookback_days"`
	MatchesNightlyRetrain bool   `json:"matches_nightly_retrain"`
	Funnel                []struct {
		Step string `json:"step"`
		Rows int64  `json:"rows"`
	} `json:"funnel"`
	Features struct {
		TrainingRows      int `json:"training_rows"`
		UsableRows        int `json:"usable_rows"`
		DroppedIncomplete int `json:"dropped_incomplete"`
		Missing           []struct {
			Feature     string `json:"feature"`
			MissingRows int    `json:"missing_rows"`
		} `json:"missing"`
	} `json:"features"`
	LastRetrain struct {
		LastRun    string `json:"last_run"`
		LatestSkip *struct {
			SkippedAt         string `json:"skipped_at"`
			ReasonCode        string `json:"reason_code"`
			UsableRows        int    `json:"usable_rows"`
			RequiredRows      int    `json:"required_rows"`
			DroppedIncomplete *int   `json:"dropped_incomplete"`
		} `json:"latest_skip"`
		LatestModelTrainedAt   *string `json:"latest_model_trained_at"`
		DroppedIncompleteState string  `json:"dropped_incomplete_status"`
	} `json:"last_retrain"`
	Split struct {
		LLMCutoff       string  `json:"llm_cutoff"`
		LLMCutoffStatus string  `json:"llm_cutoff_status"`
		OOSStart        string  `json:"oos_start"`
		TrainReady      bool    `json:"train_ready"`
		TrainSkipCode   *string `json:"train_skip_code"`
		Train           struct {
			Rows                    int  `json:"rows"`
			RequiredRows            int  `json:"required_rows"`
			FitRows                 *int `json:"fit_rows"`
			CalibrationRows         *int `json:"calibration_rows"`
			RequiredFitRows         int  `json:"required_fit_rows"`
			RequiredCalibrationRows int  `json:"required_calibration_rows"`
		} `json:"train"`
		OOS struct {
			Rows         int  `json:"rows"`
			RequiredRows int  `json:"required_rows"`
			Days         int  `json:"days"`
			RequiredDays int  `json:"required_days"`
			RequiredTake int  `json:"required_takes"`
			Ready        bool `json:"ready"`
		} `json:"oos"`
	} `json:"split"`
}

// fullLookback is a period that covers the retraining's whole 90-day lookback.
var fullLookback = map[string]any{"from": "2026-07-01", "to": "2026-10-19"}

// capturingStore is the model store of a Trainer that keeps what it was asked
// to save and records the skips; it replaces the database tables the nightly
// job writes, which the read-only tool never touches.
type capturingStore struct {
	model *repo.Model
	skips []repo.RetrainSkip
}

func (s *capturingStore) SaveModel(_ context.Context, m repo.Model) (repo.Model, error) {
	s.model = &m
	m.ID = 1
	return m, nil
}
func (*capturingStore) LatestActiveModel(context.Context) (repo.Model, bool, error) {
	return repo.Model{}, false, nil
}
func (*capturingStore) UpdateForward(context.Context, int64, repo.Forward) error { return nil }
func (s *capturingStore) RecordRetrainSkip(_ context.Context, k repo.RetrainSkip) error {
	s.skips = append(s.skips, k)
	return nil
}

// runNightly runs the real Trainer over the database as of now, with the
// settings loaded from the database the way the application loads them.
func runNightly(t *testing.T, w *sql.DB, now time.Time) *capturingStore {
	t.Helper()
	store := &capturingStore{}
	loader := func(ctx context.Context) (metalabelsettings.Settings, error) {
		return metalabelsettings.Load(ctx, mcpset.ReadStore{Q: w})
	}
	tr := training.NewTrainer(repo.NewRepository(w), store, loader,
		training.WithClock(func() time.Time { return now }), training.WithChangeLimits(changeguard.Limits{}),
		training.WithSkipRecorder(store))
	if err := tr.Retrain(t.Context()); err != nil {
		t.Fatalf("Retrain: %v", err)
	}
	return store
}

// readinessDB is a migrated database with the LLM cutoff set (a value other
// than the placeholder, as the operator would have) and instrument 1.
func readinessDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	seedInstrument(t, w)
	testkit.Setting(t, w, metalabelsettings.KeyLLMCutoff, `"2026-09-01"`)
	return path, w
}

func callReadiness(t *testing.T, srv *testServer, args map[string]any) readiness {
	t.Helper()
	var d readiness
	srv.call(t, "metalabel_training_readiness", args).data(t, "metalabel_training_readiness", &d)
	return d
}

// history seeds old rows (before the 7-day OOS window) and recent rows (inside
// it), all complete and labeled.
func history(t *testing.T, w *sql.DB, now time.Time, old, recent int) {
	t.Helper()
	for _, d := range learnable(old, 1, now.AddDate(0, 0, -12), "jev-1.13.0") {
		insertDecision(t, w, d)
	}
	for _, d := range learnable(recent, 10_000, now.Add(-30*time.Minute), "jev-1.13.0") {
		insertDecision(t, w, d)
	}
}

// Too little history: the nightly retraining skips, and the tool reports the
// same reason code, the same usable and required counts and the same OOS rows
// the real Trainer split the same rows into.
func TestReadinessMatchesTheNightlyRetrainingWhenItSkips(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	history(t, w, now, 80, 50)

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
	if d.Split.OOS.Rows != 50 || d.Split.OOS.Ready {
		t.Errorf("oos = %+v, want the 50 recent rows and not ready", d.Split.OOS)
	}
	// The usable rows the skip names are exactly the 80 old rows (before the OOS window).
	if skip.UsableRows != 80 {
		t.Errorf("fixture drifted: Trainer usable rows = %d, want 80", skip.UsableRows)
	}
}

// Enough history: the Trainer learns a model, and the tool's fit + calibration
// rows, OOS rows and OOS start are the ones the model was stored with.
func TestReadinessMatchesTheNightlyRetrainingWhenItLearns(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	history(t, w, now, 220, 130)

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
