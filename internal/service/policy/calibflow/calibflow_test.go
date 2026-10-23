package calibflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisiontrade"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	servicecalibration "github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/decisionnote"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func ptr[T any](v T) *T { return &v }

// now is 10s after the fixture bar's 09:31 timestamp, so the bar is fresh.
func now() time.Time { return time.Date(2026, 9, 27, 9, 31, 10, 0, time.UTC) }

type settings map[string]string

func (s settings) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

type bootstrapOn struct{}

func (bootstrapOn) PaperBootstrapEnabled(context.Context) (bool, error) { return true, nil }

func thresholds() policy.Thresholds {
	side := config.PolicyDirectionThresholds{
		MinProbability: 0.68, MinEntryQuality: "strong",
		MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
	}
	// A bucket needs 3 labeled samples.
	return policy.Thresholds{Policy: config.PolicyConfig{Long: side, Short: side, MinCalibrationSamples: 3}, MaxSpreadBps: 50, MinTurnover5mJPY: 3_000_000}
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seed inserts count labeled LONG trader decisions of version in the 0.60-0.70
// bucket (confidence 0.68).
func seed(t *testing.T, db *sql.DB, instrumentID int64, version string, count int, serial *int) {
	t.Helper()
	for range count {
		*serial++
		confidence := 0.68
		d, err := judgement.NewDecisionRepository(db).Insert(context.Background(), domain.JevDecision{
			InstrumentID: instrumentID, Symbol: "7203", Timestamp: now().Add(-time.Duration(*serial) * time.Hour),
			DecisionType: domain.JevDecisionTypeTrader, StateHash: fmt.Sprintf("seed-%d", *serial), StateJSON: "{}",
			QuestionVersion: version, ResponseJSON: "{}", Direction: ptr(domain.JevDirectionLong), Confidence: &confidence, ModelID: "m",
		})
		if err != nil {
			t.Fatalf("seed %s decision: %v", version, err)
		}
		if _, err := calibration.NewCalibrationRepository(db).Insert(context.Background(), domain.CalibrationOutcome{
			JevDecisionID: d.ID, HorizonMinutes: 5, FutureReturn: 1, WasDirectionCorrect: ptr(true),
		}); err != nil {
			t.Fatalf("seed %s outcome: %v", version, err)
		}
	}
}

// runPaperBootstrap runs one jev-trader job (Jev answers LONG at confidence 0.68,
// the 0.60-0.70 bucket) through the Paper bootstrap rule - the legacy threshold
// rule, which owns the bucket's sample check - over labeled trader samples per
// version, for the live version live ("" is the build default trader-v3), and
// returns the persisted signal.
func runPaperBootstrap(t *testing.T, live string, samples map[string]int) domain.TradeSignal {
	t.Helper()
	db := newDB(t)
	decisions := judgement.NewDecisionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	signals := trading.NewSignalRepository(db)
	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price: 2110.5, SpreadBps: ptr(10.0), Volume: 1000, Turnover: 2_110_500, Feature: policytest.Feature(),
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	serial := 0
	for version, count := range samples {
		seed(t, db, inst.ID, version, count, &serial)
	}

	store := settings{}
	if live != "" {
		store[jevshadowsettings.KeyLiveVersion] = `"` + live + `"`
	}
	outcomes := calibration.NewCalibrationRepository(db)
	outcomes.SetForeignTraderVersions(jev.LiveCohortScope(store))

	engine := policy.NewEngine(thresholds(), nil, signals,
		policytest.Option(policytest.Models{}, &policytest.Predictions{}), policy.WithPaperBootstrap(metalabelsettings.ModePaper, bootstrapOn{}))
	server := httptest.NewServer(jevtest.TraderHandler(jev.TraderResponse{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityStrong,
		ToxicFlow: 0.35, LiquidityStressed: 0.25, ContinuationProbability: 0.60, Confidence: 0.68, ModelID: "test-model",
	}))
	t.Cleanup(server.Close)
	trader := jev.NewTrader(jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1}), decisions, rag.NewService(db, decisions, snapshots))
	handler := policy.NewHandler(trader, snapshots, engine, nil, policy.WithClock(now), policy.WithTDnetScorer(policytest.TDnet(0)),
		policy.WithCalibration(servicecalibration.NewService(outcomes, decisiontrade.New(db))),
		policy.WithCalibrationScope(metalabelsettings.ModePaper, func(ctx context.Context) (string, error) { return jev.ResolveLiveVersion(ctx, store) }, servicecalibration.NewService(outcomes.AllVersions(), decisiontrade.New(db))))

	payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: "7203"})
	if err := handler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	got, err := signals.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListByInstrument = %v, %v; want one signal", got, err)
	}
	return got[0]
}

func reasonText(sig domain.TradeSignal) string {
	if sig.RejectReason == nil {
		return ""
	}
	return *sig.RejectReason
}

// Issue #881, Paper: the bucket check counts the live version's cohort. While
// trader-v4's own samples are too few Paper may decide on the total over every
// version, and the persisted decision (trade_signals.reject_reason, the only
// persisted text of a Policy decision) says so. trader-v1..v3 are one cohort, so
// with the live version trader-v3 the result is what it was before (v4 rows do
// not exist until a promotion).
func TestPaperBootstrap_CalibrationCheckCountsTheLiveVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		live         string
		samples      map[string]int
		wantLong     bool
		wantReason   []string // each must appear in reject_reason
		wantNoReason bool
	}{
		"live v4, own samples too few, total enough: Paper falls back and says so": {
			live: "trader-v4", samples: map[string]int{"trader-v3": 3, "trader-v4": 1}, wantLong: true,
			wantReason: []string{decisionnote.CalibrationAllVersions, "live version trader-v4 has fewer than 3 labeled samples"},
		},
		"live v4, own samples enough: passes on them, nothing recorded": {
			live: "trader-v4", samples: map[string]int{"trader-v3": 1, "trader-v4": 3}, wantLong: true, wantNoReason: true,
		},
		"live v4, own and total too few: not calibrated, names the version": {
			live: "trader-v4", samples: map[string]int{"trader-v3": 1, "trader-v4": 1},
			wantReason: []string{policy.ReasonNotCalibrated, "live version trader-v4"},
		},
		"live v3, v1 v2 v3 count together: unchanged from before": {
			samples: map[string]int{"trader-v1": 1, "trader-v2": 1, "trader-v3": 1}, wantLong: true, wantNoReason: true,
		},
		"live v3, too few: not calibrated, naming trader-v3": {
			samples: map[string]int{"trader-v3": 2}, wantReason: []string{policy.ReasonNotCalibrated, "live version trader-v3"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			sig := runPaperBootstrap(t, tc.live, tc.samples)
			if (sig.Direction == domain.JevDirectionLong) != tc.wantLong || sig.RiskPassed != tc.wantLong {
				t.Fatalf("signal = %+v (%s), want a LONG entry: %v", sig, reasonText(sig), tc.wantLong)
			}
			if tc.wantNoReason && sig.RejectReason != nil {
				t.Errorf("reject_reason = %q, want none", reasonText(sig))
			}
			for _, want := range tc.wantReason {
				if !strings.Contains(reasonText(sig), want) {
					t.Errorf("reject_reason = %q, want it to contain %q", reasonText(sig), want)
				}
			}
			if sig.PolicyVersion != "policy-vb" {
				t.Errorf("PolicyVersion = %q, want policy-vb (the bootstrap rule)", sig.PolicyVersion)
			}
		})
	}
}
