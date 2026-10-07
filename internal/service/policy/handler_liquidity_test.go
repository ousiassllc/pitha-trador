package policy_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

type fakeCalibration struct{ sampleCounts [5]int }

func (f fakeCalibration) BucketHasSamples(_ context.Context, confidence float64, min int) (bool, error) {
	for i, r := range domain.DefaultConfidenceBucketRanges {
		if confidence >= r.Low && confidence < r.High {
			return f.sampleCounts[i] >= min, nil
		}
	}
	return false, nil
}

// runHandleJob runs one jev-trader job for a fresh instrument whose latest
// snapshot carries turnover5m (nil = insufficient history) and returns the
// persisted signal.
func runHandleJob(t *testing.T, turnover5m *float64, calib policy.CalibrationSource) domain.TradeSignal {
	t.Helper()
	db := newHandlerTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	snapshots := market.NewSnapshotRepository(db)
	signals := trading.NewSignalRepository(db)
	client := jev.NewClient(jev.Config{BaseURL: traderServer(t, passingTraderResponse(domain.JevDirectionLong)).URL, MaxAttempts: 1})
	trader := jev.NewTrader(client, decisions, rag.NewService(db, decisions, snapshots))

	th := testThresholds() // MinTurnover5mJPY: 3,000,000
	th.Policy.MinCalibrationSamples = 20
	opts := []policy.HandlerOption{policy.WithClock(fixtureNow)}
	if calib != nil {
		opts = append(opts, policy.WithCalibration(calib))
	}
	handler := policy.NewHandler(trader, snapshots, policy.NewEngine(th, nil, signals), nil, opts...)

	inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price: 2110.5, SpreadBps: ptr(10.0), Volume: 1000, Turnover: 900_000_000,
		Feature: domain.Feature{Turnover5m: turnover5m},
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

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

// Regression for #163: the live path used to leave Turnover5mJPY nil, so
// FR-POLICY-3 "板が薄い" never fired.
func TestHandler_HandleJob_ThinLiquidityFromSnapshotTurnover5m(t *testing.T) {
	sig := runHandleJob(t, ptr(1_000_000.0), nil)
	if sig.Direction != domain.JevDirectionNone || sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonThinLiquidity) {
		t.Fatalf("signal = %+v, want NONE with %s", sig, policy.ReasonThinLiquidity)
	}

	if sig := runHandleJob(t, ptr(50_000_000.0), nil); sig.Direction != domain.JevDirectionLong {
		t.Errorf("liquid instrument: Direction = %q, want LONG", sig.Direction)
	}
	if sig := runHandleJob(t, nil, nil); sig.Direction != domain.JevDirectionLong {
		t.Errorf("unknown turnover_5m must skip the check: Direction = %q, want LONG", sig.Direction)
	}
}

// Regression for #163: Calibrated was hard-wired true on the live path.
func TestHandler_HandleJob_NotCalibratedWhenConfidenceBucketHasTooFewSamples(t *testing.T) {
	// Confidence 0.68 falls in the "0.60-0.70" bucket (index 1).
	sig := runHandleJob(t, ptr(50_000_000.0), fakeCalibration{sampleCounts: [5]int{0, 19, 0, 0, 0}})
	if sig.Direction != domain.JevDirectionNone || sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonNotCalibrated) {
		t.Fatalf("signal = %+v, want NONE with %s", sig, policy.ReasonNotCalibrated)
	}

	if sig := runHandleJob(t, ptr(50_000_000.0), fakeCalibration{sampleCounts: [5]int{0, 20, 0, 0, 0}}); sig.Direction != domain.JevDirectionLong {
		t.Errorf("bucket at the minimum: Direction = %q, want LONG", sig.Direction)
	}
}
