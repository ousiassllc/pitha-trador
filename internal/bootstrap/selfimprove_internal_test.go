package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperexec"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func TestBuildServices_AppliedPolicySettingReachesLivePolicyEngine(t *testing.T) {
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")
	ctx := context.Background()
	at := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)

	direction, entryQuality := domain.JevDirectionLong, domain.JevEntryQualityStrong
	confidence, continuation, toxic, stressed := 0.75, 0.8, 0.1, 0.1
	decision, err := svc.Decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "test-model",
		Direction: &direction, Confidence: &confidence,
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	decision.EntryQuality, decision.ContinuationProbability = &entryQuality, &continuation
	decision.ToxicFlow, decision.LiquidityStressed = &toxic, &stressed
	spread, price := 5.0, 2500.0
	// Risk Engine fails closed without the instrument's latest snapshot.
	if _, err := svc.Snapshots.Insert(ctx, domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, Price: price, SpreadBps: &spread, RawDataJSON: "{}",
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	in := policy.Input{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at, Decision: &decision,
		SpreadBps: &spread, EntryPriceReference: &price, Calibrated: true,
	}

	if sig, err := svc.Policy.Evaluate(ctx, in); err != nil || sig.Direction != domain.JevDirectionLong {
		t.Fatalf("Evaluate before override = (%q, %v), want LONG under strategy.yaml's thresholds", sig.Direction, err)
	}

	// What Governor writes when it applies a proposal (FR-SELFIMPROVE-5).
	if err := svc.Settings.Set(ctx, domain.PolicyKeyLongMinProbability, "0.80", at); err != nil {
		t.Fatalf("settings.Set: %v", err)
	}
	if sig, err := svc.Policy.Evaluate(ctx, in); err != nil || sig.Direction != domain.JevDirectionNone {
		t.Fatalf("Evaluate after override = (%q, %v), want NONE once long.min_probability is 0.80", sig.Direction, err)
	}
}

func TestBuildServices_RegistersSelfImproveHandler(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()
	job, err := svc.Jobs.Enqueue(ctx, jobqueue.JobQueueAnalytics, "{}", time.Now().UTC())
	if err != nil {
		t.Fatalf("Enqueue analytics job: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := svc.Scheduler.Start(runCtx, time.Hour); err != nil {
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer svc.Scheduler.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := svc.Jobs.Get(ctx, job.ID)
		if err != nil {
			t.Fatalf("Get job: %v", err)
		}
		switch got.Status {
		case jobqueue.JobStatusSucceeded:
			return
		case jobqueue.JobStatusFailed:
			lastErr := ""
			if got.LastError != nil {
				lastErr = *got.LastError
			}
			t.Fatalf("analytics job failed: %s", lastErr)
		}
		if time.Now().After(deadline) {
			t.Fatalf("analytics job status = %q after 10s, want the registered self-improvement handler to run it", got.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Not one lot fits max_position_per_symbol_pct (600,000 yen) at 400,000 yen:
// no order is submitted.
func TestPaperExecutor_SkipsEntryWhenNoLotFitsLimits(t *testing.T) {
	svc := newTestServices(t)
	inst := mustCreateInstrument(t, svc, "7203")
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Price: 400_000, Timestamp: time.Now().UTC()}
	if err := (paperexec.Executor{Engine: svc.Execution, Sizer: svc.Risk}).ExecuteSignal(context.Background(), approvedLongSignal(inst), snap); err != nil {
		t.Fatalf("ExecuteSignal = %v, want nil (skipped)", err)
	}
	if orders, err := svc.Orders.List(context.Background(), "", 10); err != nil || len(orders) != 0 {
		t.Fatalf("orders = (%+v, %v), want none submitted", orders, err)
	}
}
