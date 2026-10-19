package query_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Every non-risk reject_reason prefix the Policy Engine can write is reported
// as itself, and every meta_skip reason of the model as meta_skip:<reason>;
// this fails to compile when one of those constants is renamed, and the table
// is where a new one must be added.
func TestRejectCode_KnowsEveryPolicyPrefixAndMetaSkipReason(t *testing.T) {
	for _, head := range []string{
		policy.ReasonJevNone, policy.ReasonProbabilityBelowThreshold, policy.ReasonEntryQualityBelowThreshold,
		policy.ReasonContinuationProbabilityBelowThreshold, policy.ReasonToxicFlowAboveThreshold,
		policy.ReasonLiquidityStressedAboveThreshold, policy.ReasonSpreadTooWide, policy.ReasonThinLiquidity,
		policy.ReasonSpecialQuote, policy.ReasonPriceLimit, policy.ReasonNotLendable, policy.ReasonMissingData,
		policy.ReasonAPIError, policy.ReasonNotCalibrated, policy.ReasonMetaModelUnavailable,
		policy.ReasonVWAPZUnavailable, policy.ReasonVWAPNotExtreme,
	} {
		if got := query.RejectCode(head, ""); got != head {
			t.Errorf("RejectCode(%q) = %q, want the prefix itself", head, got)
		}
	}
	for _, reason := range []string{
		metalabel.ReasonNoModel, metalabel.ReasonModelUnusable, metalabel.ReasonNoCandidate,
		metalabel.ReasonMissingFeature, metalabel.ReasonInvalidConfig, metalabel.ReasonPredictFailed, metalabel.ReasonEVBelowMin,
	} {
		if got, want := query.RejectCode(policy.ReasonMetaSkip, reason), "meta_skip:"+reason; got != want {
			t.Errorf("RejectCode(meta_skip, %q) = %q, want %q", reason, got, want)
		}
	}
}

func TestRejectCode_UnknownAndEmptyFallIntoFixedBuckets(t *testing.T) {
	for _, c := range []struct{ head, sub, want string }{
		{"", "", "unknown"},
		{"policy_below_threshold", "", "other"},
		{"SECRET_TOKEN=abc123", "", "other"},
		{"risk_engine_rejected", "", "other"}, // risk rows are the buckets, never here
		{"meta_skip", "", "meta_skip:other"},
		{"meta_skip", "p=0.51", "meta_skip:other"},
		{"meta_skip", "someday_new_reason", "meta_skip:other"},
	} {
		if got := query.RejectCode(c.head, c.sub); got != c.want {
			t.Errorf("RejectCode(%q, %q) = %q, want %q", c.head, c.sub, got, c.want)
		}
	}
}
