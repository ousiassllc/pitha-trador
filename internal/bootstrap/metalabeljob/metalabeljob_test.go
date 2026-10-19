package metalabeljob

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

func newJob(t *testing.T) (*Job, *system.RuntimeSettingsRepository) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := system.NewRuntimeSettingsRepository(db)
	return New(db, settings), settings
}

// clockAt returns a clock fixed at the given JST wall time.
func clockAt(hhmm string) func() time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-14 "+hhmm, jst)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func TestJob_RunsOnlyFromTheSettingsRetrainTime(t *testing.T) {
	for name, tc := range map[string]struct {
		at, now string
		due     bool
	}{
		"before the retrain time":     {"18:00", "17:59", false},
		"exactly at the retrain time": {"18:00", "18:00", true},
		"after the retrain time":      {"18:00", "23:30", true},
		"early morning time, passed":  {"02:30", "03:00", true},
		"early morning time, early":   {"02:30", "01:00", false},
	} {
		job, settings := newJob(t)
		if err := settings.Set(context.Background(), metalabelsettings.KeyRetrainTime, `"`+tc.at+`"`, time.Now()); err != nil {
			t.Fatalf("%s: Set: %v", name, err)
		}
		job.now = clockAt(tc.now)
		err := job.Run(context.Background())
		if tc.due && err != nil {
			t.Errorf("%s: Run = %v, want a (no-history) success", name, err)
		}
		if !tc.due && !errors.Is(err, maintenance.ErrSkipped) {
			t.Errorf("%s: Run = %v, want maintenance.ErrSkipped", name, err)
		}
	}
}

func TestJob_RetrainTimeIsJSTNotTheHostZone(t *testing.T) {
	job, _ := newJob(t) // default retrain time 18:00
	// 09:30 UTC is 18:30 JST, 08:30 UTC is 17:30 JST.
	job.now = func() time.Time { return time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC) }
	if err := job.Run(context.Background()); err != nil {
		t.Errorf("18:30 JST: Run = %v, want to run", err)
	}
	job.now = func() time.Time { return time.Date(2026, 10, 14, 8, 30, 0, 0, time.UTC) }
	if err := job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Errorf("17:30 JST: Run = %v, want ErrSkipped", err)
	}
}

// A retraining with no labeled decisions is the normal early-days no-op: it
// succeeds and stores no model, so inference stays a skip. The skip itself is
// recorded with a fixed code and the shortfall (issue #799), and the
// Calibration source lists it.
func TestJob_NoHistoryStoresNoModelButRecordsTheSkip(t *testing.T) {
	job, _ := newJob(t)
	job.now = clockAt("20:00")
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, found, err := metalabel.NewRepository(job.db).LatestActiveModel(context.Background()); err != nil || found {
		t.Fatalf("LatestActiveModel found = %v, err %v, want no model", found, err)
	}
	skips, err := metalabel.NewRepository(job.db).ListRetrainSkips(context.Background(), 10)
	if err != nil || len(skips) != 1 {
		t.Fatalf("ListRetrainSkips = %v, %v, want exactly one recorded skip", skips, err)
	}
	if got := skips[0]; got.ReasonCode != metalabel.SkipTooFewRowsBeforeOOS || got.UsableRows != 0 || got.RequiredRows != 130 {
		t.Errorf("skip = %+v, want too_few_rows_before_oos 0/130", got)
	}
}

func TestJob_BadSettingsFailWithoutRunning(t *testing.T) {
	job, settings := newJob(t)
	job.now = clockAt("20:00")
	// A stored retrain time that no longer parses makes Load fall back to the
	// defaults, so the job still runs; an unreadable settings store fails it.
	if err := settings.Set(context.Background(), metalabelsettings.KeyRetrainTime, `"not a time"`, time.Now()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Errorf("Run with a bad stored retrain time = %v, want the default to apply", err)
	}
	if _, err := retrainDue(time.Now(), "6pm"); err == nil {
		t.Error("retrainDue accepted a malformed time")
	}
}

func TestJob_TaskIsRegisteredUnderItsName(t *testing.T) {
	job, _ := newJob(t)
	task := job.Task()
	if task.Name != TaskName || task.Label == "" || task.Run == nil {
		t.Fatalf("Task = %+v", task)
	}
}

// Without a stored model the wired Policy Engine never enters: Evaluate
// persists a NONE whose reason is the model-less skip, even for a candidate
// that clears every guard.
func TestPolicyOption_NoStoredModelIsSafeSideNone(t *testing.T) {
	job, settings := newJob(t)
	ctx := context.Background()
	instrument, err := market.NewInstrumentRepository(job.db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("Create instrument: %v", err)
	}
	f := func(v float64) *float64 { return &v }
	str := func(v string) *string { return &v }
	saved, err := judgement.NewDecisionRepository(job.db).Insert(ctx, domain.JevDecision{
		InstrumentID: instrument.ID, Symbol: "7203", Timestamp: time.Now(), DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "test-model",
	})
	if err != nil {
		t.Fatalf("Insert decision: %v", err)
	}
	side := config.PolicyDirectionThresholds{MinEntryQuality: "strong", MinContinuationProbability: 0.6, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25}
	th := policy.Thresholds{Policy: config.PolicyConfig{Long: side, Short: side}, MaxSpreadBps: 50}
	engine := policy.NewEngine(th, nil, trading.NewSignalRepository(job.db), PolicyOption(job.db, settings))

	sig, err := engine.Evaluate(ctx, policy.Input{
		InstrumentID: instrument.ID, Symbol: "7203", Timestamp: time.Now(), SpreadBps: f(10), EntryPriceReference: f(2000),
		Decision: &domain.JevDecision{
			ID: saved.ID, Direction: str(domain.JevDirectionLong), EntryQuality: str(domain.JevEntryQualityStrong), Confidence: f(0.99),
			ContinuationProbability: f(0.9), ToxicFlow: f(0.1), LiquidityStressed: f(0.1),
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Direction != domain.JevDirectionNone || sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonMetaSkip) {
		t.Fatalf("signal = %+v, want NONE with %s even at confidence 0.99", sig, policy.ReasonMetaSkip)
	}
	if sig.PolicyVersion != policy.MetaVersion {
		t.Errorf("PolicyVersion = %q, want %q", sig.PolicyVersion, policy.MetaVersion)
	}
}
