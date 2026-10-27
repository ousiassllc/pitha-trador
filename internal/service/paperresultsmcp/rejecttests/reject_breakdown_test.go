package rejecttests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/apierror"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testkit.RemoveTemplate()
	os.Exit(code)
}

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

// Issue #912: every failure kind the policy can record is its own code; a kind
// this version does not know, or none at all, stays in the plain api_error bucket.
func TestRejectCode_SplitsApiErrorByEveryKnownKind(t *testing.T) {
	for _, kind := range apierror.Codes {
		if got, want := query.RejectCode(policy.ReasonAPIError, kind), "api_error:"+kind; got != want {
			t.Errorf("RejectCode(api_error, %q) = %q, want %q", kind, got, want)
		}
	}
	for _, sub := range []string{"", "someday_new_kind", "(jev:"} {
		if got := query.RejectCode(policy.ReasonAPIError, sub); got != "api_error" {
			t.Errorf("RejectCode(api_error, %q) = %q, want api_error", sub, got)
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

// breakdownOf stores one trade_signals row per reject_reason (risk_passed = 0,
// a NONE signal, as the Policy Engine writes it) and returns risk_rejections'
// answer for them, with the breakdown as code -> count.
func breakdownOf(t *testing.T, reasons ...string) (query.RiskRejections, map[string]int) {
	t.Helper()
	_, w := testkit.NewMigratedDB(t)
	for i, reason := range reasons {
		testkit.TradeSignal(t, w, int64(i+1), fmt.Sprintf("2026-10-02T00:%02d:%02dZ", i/60, i%60), "policy-v1", false, reason)
	}
	return rejectionsOf(t, w)
}

func rejectionsOf(t *testing.T, w *sql.DB) (query.RiskRejections, map[string]int) {
	t.Helper()
	got, err := query.Rejections(t.Context(), w, query.Range{
		From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]int{}
	for _, b := range got.OtherRejectBreakdown {
		if _, dup := codes[b.Reason]; dup {
			t.Errorf("code %q is listed twice", b.Reason)
		}
		codes[b.Reason] = b.Count
	}
	return got, codes
}

// viewSum adds up the codes of one view of the breakdown: all of them but the
// ones with the prefix of the other view.
func viewSum(codes map[string]int, otherView string) int {
	sum := 0
	for code, n := range codes {
		if !strings.HasPrefix(code, otherView) {
			sum += n
		}
	}
	return sum
}

// Issue #922: a jev_none row with the regime and the confidence of Jev's answer
// (written since #912) is counted once by regime and once by confidence band,
// not by both together; a row from before #912 stays jev_none, and a regime
// that is not a known one or a malformed row is jev_none:other. Each view adds
// up to other_reject_count on its own.
func TestRejections_SplitsJevNoneByRegimeAndByConfidenceBand(t *testing.T) {
	got, codes := breakdownOf(t,
		"jev_none: confidence=0.8125 regime=RANGE",
		"jev_none: confidence=0.8125 regime=RANGE",
		"jev_none: confidence=0.5500 regime=TREND",
		"jev_none: confidence=0.7000 regime=BREAKOUT",
		"jev_none: confidence=0.3000 regime=CHAOTIC",
		"jev_none: confidence=0.5000 regime=TREND; calibration_all_versions: judged on all-versions total", // a note after the answer
		"jev_none", // from before #912
		"jev_none",
		"jev_none: something an older version wrote",       // no confidence= at all
		"jev_none: confidence=0.6000 regime=unknown",       // the policy's word for no regime
		"jev_none: confidence=0.6000 regime=range",         // names are the domain's, upper case
		"jev_none: confidence=0.6000 regime=SECRET_REGIME", // not a known regime
		"jev_none: confidence=0.6000",                      // no regime
		"jev_none: confidence=0.6000 regime=",              // cut short
		"jev_none: confidence=abc regime=RANGE",
		"api_error: timeout (x)",
		"special_quote",
		"risk_engine_rejected: kill_switch_active: [operator_manual]", // Risk's, not here
	)
	if got.OtherRejectCount != 17 {
		t.Fatalf("other_reject_count = %d, want 17", got.OtherRejectCount)
	}
	want := map[string]int{
		"jev_none:regime=RANGE": 2, "jev_none:regime=TREND": 2, "jev_none:regime=BREAKOUT": 1, "jev_none:regime=CHAOTIC": 1,
		"jev_none:conf=ge_0.8": 2, "jev_none:conf=0.4_to_0.6": 2, "jev_none:conf=0.6_to_0.8": 1, "jev_none:conf=lt_0.4": 1,
		"jev_none": 3, "jev_none:other": 6, "api_error:timeout": 1, "special_quote": 1,
	}
	if fmt.Sprint(codes) != fmt.Sprint(want) { // fmt prints a map in key order
		t.Errorf("other_reject_breakdown = %v, want %v", codes, want)
	}
	if s := viewSum(codes, "jev_none:conf="); s != got.OtherRejectCount {
		t.Errorf("the by-regime view adds up to %d, other_reject_count = %d", s, got.OtherRejectCount)
	}
	if s := viewSum(codes, "jev_none:regime="); s != got.OtherRejectCount {
		t.Errorf("the by-confidence-band view adds up to %d, other_reject_count = %d", s, got.OtherRejectCount)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"confidence=", "regime=SECRET_REGIME", "SECRET_REGIME", "0.8125", "0.5500", "abc", "calibration_all_versions"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("response contains %q: %s", leak, body)
		}
	}
}

// A confidence on the edge of a band is in the band above it; one outside
// [0, 1] or not a plain decimal number is not a confidence and the row is
// jev_none:other.
func TestRejections_JevNoneConfidenceBandBoundaries(t *testing.T) {
	for _, c := range []struct{ conf, want string }{
		{"0.0000", "jev_none:conf=lt_0.4"},
		{"0", "jev_none:conf=lt_0.4"},
		{"0.3999", "jev_none:conf=lt_0.4"},
		{"0.4000", "jev_none:conf=0.4_to_0.6"},
		{"0.4", "jev_none:conf=0.4_to_0.6"},
		{"0.5999", "jev_none:conf=0.4_to_0.6"},
		{"0.6000", "jev_none:conf=0.6_to_0.8"},
		{"0.7999", "jev_none:conf=0.6_to_0.8"},
		{"0.8000", "jev_none:conf=ge_0.8"},
		{"0.9999", "jev_none:conf=ge_0.8"},
		{"1.0000", "jev_none:conf=ge_0.8"},
		{"1", "jev_none:conf=ge_0.8"},
		{"1.0001", "jev_none:other"},
		{"2", "jev_none:other"},
		{"-0.1000", "jev_none:other"},
		{"+0.5000", "jev_none:other"},
		{"NaN", "jev_none:other"},
		{"Inf", "jev_none:other"},
		{"1e-1", "jev_none:other"},
		{"0.5.5", "jev_none:other"},
		{"0,5", "jev_none:other"},
		{"", "jev_none:other"},
	} {
		got, codes := breakdownOf(t, "jev_none: confidence="+c.conf+" regime=RANGE")
		wantCodes := map[string]int{c.want: 1}
		if c.want != "jev_none:other" {
			wantCodes["jev_none:regime=RANGE"] = 1
		}
		if fmt.Sprint(codes) != fmt.Sprint(wantCodes) || got.OtherRejectCount != 1 {
			t.Errorf("confidence=%q: breakdown = %v (other_reject_count %d), want %v", c.conf, codes, got.OtherRejectCount, wantCodes)
		}
	}
}

// Every regime Jev can answer with is its own code.
func TestRejections_JevNoneKnowsEveryRegime(t *testing.T) {
	regimes := []string{domain.JevRegimeTrend, domain.JevRegimeRange, domain.JevRegimeBreakout, domain.JevRegimeChaotic}
	var reasons []string
	for _, r := range regimes {
		reasons = append(reasons, "jev_none: confidence=0.5000 regime="+r)
	}
	_, codes := breakdownOf(t, reasons...)
	for _, r := range regimes {
		if codes["jev_none:regime="+r] != 1 {
			t.Errorf("regime %s: breakdown = %v, want jev_none:regime=%s = 1", r, codes, r)
		}
	}
	if len(codes) != len(regimes)+1 || codes["jev_none:conf=0.4_to_0.6"] != len(regimes) {
		t.Errorf("breakdown = %v, want the regimes and one band", codes)
	}
}
