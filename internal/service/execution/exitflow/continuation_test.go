package exitflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
)

// fakeEntryThresholds is a static execution.EntryThresholdSource.
type fakeEntryThresholds struct {
	policy config.PolicyConfig
	err    error
}

func (f fakeEntryThresholds) CurrentThresholds(context.Context) (config.PolicyConfig, error) {
	return f.policy, f.err
}

func entryThresholds(long, short float64) fakeEntryThresholds {
	return fakeEntryThresholds{policy: config.PolicyConfig{
		Long:  config.PolicyDirectionThresholds{MinContinuationProbability: long},
		Short: config.PolicyDirectionThresholds{MinContinuationProbability: short},
	}}
}

// TestEngine_EvaluateExit_ContinuationThresholdFollowsEntry covers
// FR-EXIT-2: the continuation_probability低下 threshold is
// min(Config.MinContinuationProbability, the position side's entry
// threshold), so a lowered entry threshold cannot cause an immediate exit
// (#714) while the default and a stricter entry threshold leave 0.60.
func TestEngine_EvaluateExit_ContinuationThresholdFollowsEntry(t *testing.T) {
	openedAt := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	cfg := execution.Config{StopLossPct: 0.6, TakeProfitPct: 1.2, MinContinuationProbability: 0.60}
	long, short := domain.JevDirectionLong, domain.JevDirectionShort

	tests := []struct {
		name         string
		side         string
		source       execution.EntryThresholdSource
		continuation float64
		want         string // "" = no exit
	}{
		{"LONG entry 0.55: 0.57 does not exit", domain.PositionSideLong, entryThresholds(0.55, 0.60), 0.57, ""},
		{"LONG entry 0.55: 0.54 exits", domain.PositionSideLong, entryThresholds(0.55, 0.60), 0.54, domain.ExitReasonContinuationProbDrop},
		{"SHORT entry 0.55: 0.57 does not exit", domain.PositionSideShort, entryThresholds(0.60, 0.55), 0.57, ""},
		{"SHORT entry 0.55: 0.54 exits", domain.PositionSideShort, entryThresholds(0.60, 0.55), 0.54, domain.ExitReasonContinuationProbDrop},
		{"LONG ignores the SHORT entry threshold", domain.PositionSideLong, entryThresholds(0.60, 0.50), 0.57, domain.ExitReasonContinuationProbDrop},
		{"default entry 0.60: 0.59 exits", domain.PositionSideLong, entryThresholds(0.60, 0.60), 0.59, domain.ExitReasonContinuationProbDrop},
		{"default entry 0.60: 0.60 does not exit", domain.PositionSideLong, entryThresholds(0.60, 0.60), 0.60, ""},
		{"stricter entry 0.70: exit stays 0.60 (0.65 holds)", domain.PositionSideLong, entryThresholds(0.70, 0.70), 0.65, ""},
		{"stricter entry 0.70: exit stays 0.60 (0.59 exits)", domain.PositionSideLong, entryThresholds(0.70, 0.70), 0.59, domain.ExitReasonContinuationProbDrop},
		{"no source: Config value applies", domain.PositionSideLong, nil, 0.59, domain.ExitReasonContinuationProbDrop},
		{"source error: falls back to Config value", domain.PositionSideLong, fakeEntryThresholds{err: errors.New("db down")}, 0.57, domain.ExitReasonContinuationProbDrop},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			te := newTestEngineWithThresholds(t, cfg, tc.source)
			direction := &long
			signal := longSignal(te.instrument.ID)
			if tc.side == domain.PositionSideShort {
				direction = &short
				signal.Direction = domain.JevDirectionShort
			}
			result, err := te.engine.Enter(context.Background(), execution.EntryRequest{
				Signal: signal, Quantity: 100, OrderType: domain.OrderTypeMarket, Price: 2100.0, Now: openedAt,
			})
			if err != nil {
				t.Fatalf("Enter (fixture): %v", err)
			}
			mkt := execution.MarketContext{
				Price: 2100.0, Now: openedAt.Add(time.Minute),
				Decision: &domain.JevDecision{Direction: direction, ContinuationProbability: &tc.continuation},
			}
			reason, triggered, err := te.engine.EvaluateExit(context.Background(), *result.Position, mkt)
			if err != nil {
				t.Fatalf("EvaluateExit: %v", err)
			}
			if reason != tc.want || triggered != (tc.want != "") {
				t.Fatalf("EvaluateExit() = (%q, %v), want (%q, %v)", reason, triggered, tc.want, tc.want != "")
			}
		})
	}
}
