package labels_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/apierror"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// Every code risk_rejections' other_reject_breakdown can return (the head of a
// Policy Engine rejection, meta_skip with its reason, "other", "unknown") reads
// as a name, never as the code and never as the unknown-code fallback (issue
// #904: the experiments' comparison lists them).
func TestPolicyRejectCode_NamesEveryCodeTheQueryReturns(t *testing.T) {
	var codes []string
	for head := range labels.RejectReasons {
		if head != "risk_engine_rejected" { // the Risk Engine's rows are the other buckets
			codes = append(codes, query.RejectCode(head, ""))
		}
	}
	for _, sub := range []string{"no_active_model", "model_unusable", "no_candidate", "missing_feature", "invalid_config", "predict_failed", "ev_not_above_minimum", "never_heard_of"} {
		codes = append(codes, query.RejectCode("meta_skip", sub))
	}
	for _, kind := range apierror.Codes {
		codes = append(codes, query.RejectCode("api_error", kind))
	}
	codes = append(codes, query.RejectCode("", ""), query.RejectCode("some_new_head", ""))
	for _, regime := range []string{"TREND", "RANGE", "BREAKOUT", "CHAOTIC"} {
		codes = append(codes, "jev_none:regime="+regime)
	}
	for _, band := range []string{"lt_0.4", "0.4_to_0.6", "0.6_to_0.8", "ge_0.8"} {
		codes = append(codes, "jev_none:conf="+band)
	}
	codes = append(codes, "jev_none:other")

	for _, code := range codes {
		name := labels.PolicyRejectCode(code)
		if name == labels.Unknown || name == labels.Other || strings.ContainsAny(name, "_:") {
			t.Errorf("PolicyRejectCode(%q) = %q, want a plain name", code, name)
		}
	}
	for code, want := range map[string]string{
		"jev_none":                      "Jev が方向なしと判断",
		"meta_skip:no_active_model":     "メタモデルが見送り（採用中のモデルがない）",
		"meta_skip:other":               "メタモデルが見送り（その他）",
		"other":                         "その他の理由",
		"unknown":                       "理由の記録なし",
		"api_error":                     "API エラー",
		"api_error:timeout":             "API エラー（時間切れ）",
		"api_error:persist_failed":      "API エラー（判断を保存できない）",
		"meta_skip:a_reason_not_listed": "メタモデルが見送り（その他）",
		"jev_none:regime=RANGE":         "Jev が方向なしと判断（局面 RANGE）",
		"jev_none:conf=lt_0.4":          "Jev が方向なしと判断（確信度 0.4 未満）",
		"jev_none:conf=ge_0.8":          "Jev が方向なしと判断（確信度 0.8 以上）",
		"jev_none:other":                "Jev が方向なしと判断（その他）",
	} {
		if got := labels.PolicyRejectCode(code); got != want {
			t.Errorf("PolicyRejectCode(%q) = %q, want %q", code, got, want)
		}
	}
}
