package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

func TestBuildServices_OutcomeLabelingFeedsCalibrationMetrics(t *testing.T) {
	svc := newTestServices(t, nil)
	inst := mustCreateInstrument(t, svc, "7203")
	ctx := context.Background()

	// One Jev Trader LONG decision followed by 20 rising 1-minute bars,
	// all far enough in the past that every horizon has elapsed.
	decidedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	for i := 0; i <= 20; i++ {
		if _, err := svc.Snapshots.Insert(ctx, domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol,
			Timestamp: decidedAt.Add(time.Duration(i) * time.Minute), Price: 2500 + float64(i),
		}); err != nil {
			t.Fatalf("insert snapshot %d: %v", i, err)
		}
	}
	direction, confidence := domain.JevDirectionLong, 0.7
	if _, err := svc.Decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: decidedAt, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}",
		Direction: &direction, Confidence: &confidence, ModelID: "test-model",
	}); err != nil {
		t.Fatalf("insert trader decision: %v", err)
	}

	enqueued, err := svc.Scheduler.EnqueueOutcomeLabeling(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnqueueOutcomeLabeling: %v", err)
	}
	want := len(scheduler.DefaultOutcomeLabelHorizonsMinutes)
	if enqueued != want {
		t.Fatalf("EnqueueOutcomeLabeling = %d, want %d (outcome labeling wired to the calibration repository)", enqueued, want)
	}

	runCtx, cancel := context.WithCancel(ctx)
	if err := svc.Scheduler.Start(runCtx, time.Hour); err != nil {
		cancel()
		t.Fatalf("Scheduler.Start: %v", err)
	}
	defer func() {
		cancel()
		svc.Scheduler.Stop()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		metrics, err := svc.Calibration.Metrics(ctx)
		if err != nil {
			t.Fatalf("Calibration.Metrics: %v", err)
		}
		if metrics.SampleCount == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Calibration SampleCount = %d, want %d once the registered outcome-labeling handler has run", metrics.SampleCount, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
