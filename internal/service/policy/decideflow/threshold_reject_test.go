package decideflow_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Issue #911: policy.ThresholdReject is the guard chain Decide runs, exported so
// the read-only entry_quality_whatif tool judges stored answers with the live
// rule instead of a copy. Whatever Decide records for a candidate that reaches
// the guards, ThresholdReject (with the probability check, as in the legacy
// rule) returns for the same thresholds and answer, and a candidate it passes
// is entered.
func TestThresholdRejectIsTheGuardChainDecideRuns(t *testing.T) {
	th := testThresholds()
	for name, mutate := range map[string]func(*domain.JevDecision){
		"clears every guard":            func(*domain.JevDecision) {},
		"probability below min":         func(d *domain.JevDecision) { *d.Confidence = 0.5 },
		"entry_quality below min":       func(d *domain.JevDecision) { *d.EntryQuality = domain.JevEntryQualityGood },
		"unknown entry_quality":         func(d *domain.JevDecision) { *d.EntryQuality = "great" },
		"continuation below min":        func(d *domain.JevDecision) { *d.ContinuationProbability = 0.4 },
		"toxic flow above max":          func(d *domain.JevDecision) { *d.ToxicFlow = 0.5 },
		"liquidity stress above max":    func(d *domain.JevDecision) { *d.LiquidityStressed = 0.3 },
		"entry_quality is judged first": func(d *domain.JevDecision) { *d.EntryQuality, *d.ToxicFlow = domain.JevEntryQualityFair, 0.9 },
		"probability is judged first":   func(d *domain.JevDecision) { *d.Confidence, *d.EntryQuality = 0.1, domain.JevEntryQualityPoor },
	} {
		t.Run(name, func(t *testing.T) {
			in := passingInput(domain.JevDirectionLong)
			mutate(in.Decision)

			sig := policy.NewEngine(th, nil, nil).Decide(context.Background(), in)
			want := policy.ThresholdReject(th.Policy.Long, in.Decision, true)

			switch {
			case want == nil && (sig.Direction != domain.JevDirectionLong || sig.RejectReason != nil):
				t.Errorf("ThresholdReject passes, Decide = %s (%v)", sig.Direction, sig.RejectReason)
			case want != nil && (sig.Direction != domain.JevDirectionNone || sig.RejectReason == nil || *sig.RejectReason != *want):
				t.Errorf("ThresholdReject = %q, Decide = %s (%v)", *want, sig.Direction, sig.RejectReason)
			}
		})
	}
}
