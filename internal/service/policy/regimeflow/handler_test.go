package regimeflow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// fixedRegime is a policy.RegimeAssessor returning one assessment.
type fixedRegime struct{ assessment marketregime.Assessment }

func (f fixedRegime) Assess(context.Context, domain.Snapshot) marketregime.Assessment {
	return f.assessment
}

// emptyStore is a regimesettings.Store with nothing stored: every default.
type emptyStore struct{}

func (emptyStore) Get(context.Context, string) (string, bool, error) { return "", false, nil }

type recordingExecutor struct{ executed []domain.TradeSignal }

func (e *recordingExecutor) ExecuteSignal(_ context.Context, signal domain.TradeSignal, _ domain.Snapshot) error {
	e.executed = append(e.executed, signal)
	return nil
}

// regimeJob runs one jev-trader job through a real Trader (against an httptest
// server answering LONG, every guard cleared) and a real Engine and Handler
// (against a migrated SQLite DB), the Handler built with the option mk makes
// (nil: none). It returns the persisted signal, the Jev decision it derives
// from and the executor.
func regimeJob(t *testing.T, mk func(db *sql.DB) policy.HandlerOption) (domain.TradeSignal, domain.JevDecision, *recordingExecutor) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	server := httptest.NewServer(jevtest.TraderHandler(jev.TraderResponse{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityStrong,
		ToxicFlow: 0.35, LiquidityStressed: 0.25, ContinuationProbability: 0.60, Confidence: 0.68, ModelID: "test-model",
	}))
	t.Cleanup(server.Close)

	decisions, snapshots, signals := judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db), trading.NewSignalRepository(db)
	trader := jev.NewTrader(jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1}), decisions, rag.NewService(db, decisions, snapshots))
	store := policytest.NewStore(t, db, policytest.TakingModel())
	engine := policy.NewEngine(testThresholds(), nil, signals, policytest.Option(store, store))
	executor := &recordingExecutor{}
	// 10s after the bar's 09:31 timestamp, so it is fresh.
	opts := []policy.HandlerOption{
		policy.WithClock(func() time.Time { return time.Date(2026, 9, 27, 9, 31, 10, 0, time.UTC) }),
		policy.WithTDnetScorer(policytest.TDnet(0)),
	}
	if mk != nil {
		opts = append(opts, mk(db))
	}
	handler := policy.NewHandler(trader, snapshots, engine, executor, opts...)

	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "7203 Inc", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price: 2110.5, SpreadBps: ptr(10.0), Volume: 1000, Turnover: 2_110_500, Feature: policytest.Feature(),
	}); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	if err != nil {
		t.Fatalf("marshal job payload: %v", err)
	}
	if err := handler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	got, err := signals.ListByInstrument(context.Background(), inst.ID, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("signals = %+v, %v, want one", got, err)
	}
	decision, err := decisions.Get(context.Background(), *got[0].JevDecisionID)
	if err != nil {
		t.Fatalf("Get decision: %v", err)
	}
	return got[0], decision, executor
}

func assessor(a marketregime.Assessment) func(*sql.DB) policy.HandlerOption {
	return func(*sql.DB) policy.HandlerOption { return policy.WithRegimeAssessor(fixedRegime{a}) }
}

// issue #755: the regime assessed before Jev's call is stored with the decision
// and, on the regime arm in a mean-reverting regime, flips the entry to the
// VWAP reversion: Jev says LONG, the price is 3 sigma above VWAP, so it sells.
func TestHandler_HandleJob_StoresRegimeAndTradesVWAPReversion(t *testing.T) {
	z, vol, ac := 3.0, 4.0, -0.5
	sig, decision, executor := regimeJob(t, assessor(marketregime.Assessment{
		Regime: marketregime.MeanRevert, ABArm: marketregime.ArmRegime, VolatilityBps: &vol, Autocorr: &ac, VWAPZ: &z, VWAPZThreshold: 2,
	}))

	if sig.Direction != domain.JevDirectionShort || !sig.RiskPassed {
		t.Fatalf("signal = %q risk_passed=%v reason=%v, want the reversion SHORT", sig.Direction, sig.RiskPassed, sig.RejectReason)
	}
	if len(executor.executed) != 1 || executor.executed[0].Direction != domain.JevDirectionShort {
		t.Fatalf("executed = %+v, want the reversion SHORT handed to the executor", executor.executed)
	}
	r := decision.MarketRegime
	if r.Regime != marketregime.MeanRevert || r.ABArm != marketregime.ArmRegime || r.VWAPZ == nil || *r.VWAPZ != z ||
		r.VolatilityBps == nil || *r.VolatilityBps != vol || r.Autocorr == nil || *r.Autocorr != ac {
		t.Fatalf("stored regime = %+v, want mean_revert / regime / z 3 / vol 4 / autocorr -0.5", r)
	}
}

// The momentum arm is stored too, and trades Jev's own direction.
func TestHandler_HandleJob_MomentumArmKeepsJevDirection(t *testing.T) {
	z := 3.0
	sig, decision, _ := regimeJob(t, assessor(marketregime.Assessment{
		Regime: marketregime.MeanRevert, ABArm: marketregime.ArmMomentum, VWAPZ: &z, VWAPZThreshold: 2,
	}))

	if sig.Direction != domain.JevDirectionLong {
		t.Fatalf("signal = %q, want Jev's LONG", sig.Direction)
	}
	if decision.MarketRegime.ABArm != marketregime.ArmMomentum || decision.MarketRegime.Regime != marketregime.MeanRevert {
		t.Fatalf("stored regime = %+v, want mean_revert / momentum", decision.MarketRegime)
	}
}

// Without an assessor nothing is assessed: no regime is stored and the plain
// momentum strategy trades.
func TestHandler_HandleJob_WithoutAssessorStoresNoRegime(t *testing.T) {
	sig, decision, _ := regimeJob(t, nil)

	if sig.Direction != domain.JevDirectionLong || decision.MarketRegime != (marketregime.Assessment{}) {
		t.Fatalf("signal = %q, stored regime = %+v, want LONG and none", sig.Direction, decision.MarketRegime)
	}
}

// WithRegimeStore is the production wiring: with no market index tracked the
// regime is unknown, and with the A/B comparison off (the default) the decision
// is the momentum arm.
func TestHandler_HandleJob_RegimeStoreAssessesFromSettings(t *testing.T) {
	sig, decision, _ := regimeJob(t, func(db *sql.DB) policy.HandlerOption {
		return policy.WithRegimeStore(market.NewInstrumentRepository(db), market.NewSnapshotRepository(db), emptyStore{}, 0)
	})

	if sig.Direction != domain.JevDirectionLong {
		t.Fatalf("signal = %q, want Jev's LONG (an unknown regime trades momentum)", sig.Direction)
	}
	if r := decision.MarketRegime; r.Regime != marketregime.Unknown || r.ABArm != marketregime.ArmMomentum {
		t.Fatalf("stored regime = %+v, want unknown / momentum", r)
	}
}
