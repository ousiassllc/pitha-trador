package policy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func testThresholds() policy.Thresholds {
	return policy.Thresholds{
		Policy: config.PolicyConfig{
			Long: config.PolicyDirectionThresholds{
				MinProbability: 0.68, MinEntryQuality: "strong",
				MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
			},
			Short: config.PolicyDirectionThresholds{
				MinProbability: 0.68, MinEntryQuality: "strong",
				MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
			},
		},
		MaxSpreadBps:     50,
		MinTurnover5mJPY: 3_000_000,
	}
}

func ptr[T any](v T) *T { return &v }

// passingDecision returns a Jev Trader decision that exactly clears every
// FR-POLICY-1/2 threshold for direction (the LONG/SHORT boundary values
// themselves: probability == 0.68, entry_quality == strong,
// continuation_probability == 0.60, toxic_flow == 0.35,
// liquidity_stressed == 0.25).
func passingDecision(direction string) domain.JevDecision {
	return domain.JevDecision{
		ID:                      7,
		Direction:               ptr(direction),
		EntryQuality:            ptr(domain.JevEntryQualityStrong),
		Confidence:              ptr(0.68),
		ContinuationProbability: ptr(0.60),
		ToxicFlow:               ptr(0.35),
		LiquidityStressed:       ptr(0.25),
	}
}

func passingInput(direction string) policy.Input {
	return policy.Input{
		InstrumentID:        1,
		Symbol:              "7203",
		Timestamp:           time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Decision:            decisionPtr(passingDecision(direction)),
		SpreadBps:           ptr(10.0),
		EntryPriceReference: ptr(2110.5),
		Calibrated:          true,
	}
}

func decisionPtr(d domain.JevDecision) *domain.JevDecision { return &d }

func TestEngine_Decide_LongAtExactBoundaryProducesLongSignal(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)

	sig := e.Decide(context.Background(), passingInput(domain.JevDirectionLong))

	if sig.Direction != domain.JevDirectionLong {
		t.Fatalf("Direction = %q, want %q", sig.Direction, domain.JevDirectionLong)
	}
	if !sig.RiskPassed {
		t.Fatalf("RiskPassed = false, want true")
	}
	if sig.RejectReason != nil {
		t.Fatalf("RejectReason = %v, want nil", sig.RejectReason)
	}
	if sig.Score == nil || *sig.Score != 0.68 {
		t.Fatalf("Score = %v, want 0.68", sig.Score)
	}
	if sig.PolicyVersion != policy.Version {
		t.Fatalf("PolicyVersion = %q, want %q", sig.PolicyVersion, policy.Version)
	}
	if sig.JevDecisionID == nil || *sig.JevDecisionID != 7 {
		t.Fatalf("JevDecisionID = %v, want 7", sig.JevDecisionID)
	}
	if sig.EntryPriceReference == nil || *sig.EntryPriceReference != 2110.5 {
		t.Fatalf("EntryPriceReference = %v, want 2110.5", sig.EntryPriceReference)
	}
}

func TestEngine_Decide_ShortAtExactBoundaryProducesShortSignal(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)

	sig := e.Decide(context.Background(), passingInput(domain.JevDirectionShort))

	if sig.Direction != domain.JevDirectionShort {
		t.Fatalf("Direction = %q, want %q", sig.Direction, domain.JevDirectionShort)
	}
	if !sig.RiskPassed {
		t.Fatalf("RiskPassed = false, want true")
	}
}

func TestEngine_Decide_NoneConditions(t *testing.T) {
	tests := map[string]struct {
		mutate           func(in *policy.Input)
		wantReasonPrefix string
	}{
		"jev direction none": {
			mutate: func(in *policy.Input) {
				in.Decision.Direction = ptr(domain.JevDirectionNone)
			},
			wantReasonPrefix: policy.ReasonJevNone,
		},
		"probability just below threshold": {
			mutate: func(in *policy.Input) {
				in.Decision.Confidence = ptr(0.6799)
			},
			wantReasonPrefix: policy.ReasonProbabilityBelowThreshold,
		},
		"entry quality below strong": {
			mutate: func(in *policy.Input) {
				in.Decision.EntryQuality = ptr(domain.JevEntryQualityGood)
			},
			wantReasonPrefix: policy.ReasonEntryQualityBelowThreshold,
		},
		"continuation probability just below threshold": {
			mutate: func(in *policy.Input) {
				in.Decision.ContinuationProbability = ptr(0.5999)
			},
			wantReasonPrefix: policy.ReasonContinuationProbabilityBelowThreshold,
		},
		"toxic flow just above threshold": {
			mutate: func(in *policy.Input) {
				in.Decision.ToxicFlow = ptr(0.3501)
			},
			wantReasonPrefix: policy.ReasonToxicFlowAboveThreshold,
		},
		"liquidity stressed just above threshold": {
			mutate: func(in *policy.Input) {
				in.Decision.LiquidityStressed = ptr(0.2501)
			},
			wantReasonPrefix: policy.ReasonLiquidityStressedAboveThreshold,
		},
		"spread too wide": {
			mutate: func(in *policy.Input) {
				in.SpreadBps = ptr(50.01)
			},
			wantReasonPrefix: policy.ReasonSpreadTooWide,
		},
		"thin liquidity": {
			mutate: func(in *policy.Input) {
				in.Turnover5mJPY = ptr(2_999_999.0)
			},
			wantReasonPrefix: policy.ReasonThinLiquidity,
		},
		"missing board data": {
			mutate: func(in *policy.Input) {
				in.SpreadBps = nil
			},
			wantReasonPrefix: policy.ReasonMissingData,
		},
		"missing jev decision": {
			mutate: func(in *policy.Input) {
				in.Decision = nil
			},
			wantReasonPrefix: policy.ReasonMissingData,
		},
		"not calibrated": {
			mutate: func(in *policy.Input) {
				in.Calibrated = false
			},
			wantReasonPrefix: policy.ReasonNotCalibrated,
		},
		"api error": {
			mutate: func(in *policy.Input) {
				in.APIErr = errors.New("jev: request timed out")
			},
			wantReasonPrefix: policy.ReasonAPIError,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := policy.NewEngine(testThresholds(), nil, nil)
			in := passingInput(domain.JevDirectionLong)
			tc.mutate(&in)

			sig := e.Decide(context.Background(), in)

			if sig.Direction != domain.JevDirectionNone {
				t.Fatalf("Direction = %q, want %q", sig.Direction, domain.JevDirectionNone)
			}
			if sig.RiskPassed {
				t.Fatalf("RiskPassed = true, want false")
			}
			if sig.Score != nil {
				t.Fatalf("Score = %v, want nil", sig.Score)
			}
			if sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, tc.wantReasonPrefix) {
				t.Fatalf("RejectReason = %v, want prefix %q", sig.RejectReason, tc.wantReasonPrefix)
			}
		})
	}
}

// thinLiquidityAllowedWhenTurnoverUnwired documents that a nil
// Turnover5mJPY (the real jev-trader queue path's current state, per
// Handler's doc comment) does not itself reject the candidate.
func TestEngine_Decide_NilTurnoverDoesNotRejectCandidate(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)
	in := passingInput(domain.JevDirectionLong)
	in.Turnover5mJPY = nil

	sig := e.Decide(context.Background(), in)

	if sig.Direction != domain.JevDirectionLong {
		t.Fatalf("Direction = %q, want %q (nil Turnover5mJPY must skip, not fail, the check)", sig.Direction, domain.JevDirectionLong)
	}
}

type fakeRiskChecker struct {
	passed bool
	reason string
}

func (f fakeRiskChecker) Check(context.Context, int64, string) (bool, string) {
	return f.passed, f.reason
}

func TestEngine_Decide_RiskEngineRejectionOverridesToNone(t *testing.T) {
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: false, reason: "max_open_positions exceeded"}, nil)

	sig := e.Decide(context.Background(), passingInput(domain.JevDirectionLong))

	if sig.Direction != domain.JevDirectionNone {
		t.Fatalf("Direction = %q, want %q", sig.Direction, domain.JevDirectionNone)
	}
	if sig.RiskPassed {
		t.Fatalf("RiskPassed = true, want false")
	}
	if sig.Score != nil {
		t.Fatalf("Score = %v, want nil", sig.Score)
	}
	if sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonRiskEngineRejected) {
		t.Fatalf("RejectReason = %v, want prefix %q", sig.RejectReason, policy.ReasonRiskEngineRejected)
	}
	if !strings.Contains(*sig.RejectReason, "max_open_positions exceeded") {
		t.Fatalf("RejectReason = %q, want it to include the RiskChecker's own reason", *sig.RejectReason)
	}
}

func TestEngine_Decide_RiskEngineNotConsultedWhenAlreadyNone(t *testing.T) {
	e := policy.NewEngine(testThresholds(), fakeRiskChecker{passed: false, reason: "should not be called"}, nil)

	in := passingInput(domain.JevDirectionLong)
	in.Decision.Direction = ptr(domain.JevDirectionNone)

	sig := e.Decide(context.Background(), in)

	if sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonJevNone) {
		t.Fatalf("RejectReason = %v, want prefix %q (risk check must not override a pre-existing NONE reason)", sig.RejectReason, policy.ReasonJevNone)
	}
}
