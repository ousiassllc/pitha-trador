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
		UsableRows        int `json:"all_usable_rows"`
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
	ExcludedPromptVersions []string `json:"excluded_prompt_versions"`
	PromptCohort           struct {
		LivePromptVersion string `json:"live_prompt_version"`
		LatestPromotion   *struct {
			At          string `json:"at"`
			FromVersion string `json:"from_version"`
			ToVersion   string `json:"to_version"`
		} `json:"latest_promotion"`
		Excluded []struct {
			PromptVersion      string `json:"prompt_version"`
			Cohort             string `json:"cohort"`
			TraderDecisions    int64  `json:"trader_decisions"`
			LongShortRows      int64  `json:"long_short_rows"`
			LabelV2Rows        int64  `json:"label_v2_rows"`
			OldestDecisionDate string `json:"oldest_decision_date"`
			NewestDecisionDate string `json:"newest_decision_date"`
		} `json:"excluded"`
	} `json:"prompt_cohort"`
	Split struct {
		LLMCutoff       string `json:"llm_cutoff"`
		LLMCutoffStatus string `json:"llm_cutoff_status"`
		OOSStart        string `json:"oos_start"`
		OOSStartReason  string `json:"oos_start_reason"`
		OOSFloor        string `json:"oos_floor"`
		OOSFloorSource  string `json:"oos_floor_source"`
		Shortfall       struct {
			OOSWindowRows           int     `json:"oos_window_rows"`
			OOSRowsMissing          int     `json:"oos_rows_missing"`
			PurgedRows              int     `json:"purged_rows"`
			TrainRowsMissing        int     `json:"train_rows_missing"`
			AdditionalRowsNeeded    int     `json:"additional_rows_needed"`
			RowsPerTradingDay       float64 `json:"rows_per_trading_day"`
			RateTradingDays         int     `json:"rate_trading_days"`
			EstimatedTradingDays    *int    `json:"estimated_trading_days"`
			OOSWindowTradingDays    int     `json:"oos_window_trading_days"`
			OOSMinRowsPerTradingDay *int    `json:"oos_min_rows_per_trading_day"`
			RateReachesOOSBar       *bool   `json:"rate_reaches_oos_bar"`
		} `json:"shortfall"`
		TrainReady    bool    `json:"train_ready"`
		TrainSkipCode *string `json:"train_skip_code"`
		Train         struct {
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

// Fixture sizes come from the retraining's own configuration, never typed as
// numbers: a change of DefaultWalkForwardConfig / DefaultAcceptanceConfig moves
// the fixtures with it.

// fitAndCalibration is the rows fit and calibration need together.
func fitAndCalibration() int {
	walk := training.DefaultWalkForwardConfig()
	return walk.MinFit + walk.MinCalibration
}

// oosBarRows is the OOS rows the acceptance bar asks for, which is also how
// many rows the OOS window reaches back to hold.
func oosBarRows() int { return training.DefaultAcceptanceConfig().MinOOSSamples }

// oldRowsAge is how far before now the old rows end: farther back than any OOS
// window the retraining uses (7 days before issue #862, up to 21 days after),
// so they are always the fit/calibration side whatever the window is. The
// walk-forward configuration's window fields differ between those two states,
// so this one age is the fixture's only duration that is not read from it.
const oldRowsAge = 25 * 24 * time.Hour

// history seeds old rows and recent rows, all complete and labeled, one every
// 20 minutes. The old rows end oldRowsAge before now (the fit/calibration
// side); the recent rows, the last one 30 minutes before now, are the OOS side.
func history(t *testing.T, w *sql.DB, now time.Time, old, recent int) {
	t.Helper()
	for _, d := range learnable(old, 1, now.Add(-oldRowsAge), "jev-1.13.0") {
		insertDecision(t, w, d)
	}
	for _, d := range learnable(recent, 10_000, now.Add(-30*time.Minute), "jev-1.13.0") {
		insertDecision(t, w, d)
	}
}

// learnableHistory is enough history for the retraining to learn a model: twice
// the fit and calibration rows, and OOS rows that meet the bar's row count over
// at least the bar's trading days.
func learnableHistory(t *testing.T, w *sql.DB, now time.Time) {
	t.Helper()
	bar := training.DefaultAcceptanceConfig()
	if days := time.Duration(oosBarRows()) * 20 * time.Minute / (24 * time.Hour); int(days) < bar.MinOOSDays {
		t.Fatalf("fixture: %d OOS rows every 20 minutes cover %d days, the bar needs %d", oosBarRows(), int(days), bar.MinOOSDays)
	}
	history(t, w, now, 2*fitAndCalibration(), oosBarRows())
}
