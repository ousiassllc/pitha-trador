package labels

import "strings"

// RejectCodeOther is the code risk_rejections' other_reject_breakdown gives a
// reason this build does not know (internal/service/paperresultsmcp/query).
const rejectCodeOther = "other"

// metaSkipReasons names what follows "meta_skip:" in the codes of
// risk_rejections' other_reject_breakdown (the internal/service/metalabel
// Decision reasons; "other" is a reason the query does not know).
var metaSkipReasons = map[string]string{
	"no_active_model":      "採用中のモデルがない",
	"model_unusable":       "モデルを使えない",
	"no_candidate":         "候補がない",
	"missing_feature":      "特徴量が足りない",
	"invalid_config":       "設定が不正",
	"predict_failed":       "予測に失敗した",
	"ev_not_above_minimum": "期待値が下限に届かない",
	rejectCodeOther:        "その他",
}

// apiErrorKinds names what follows "api_error:" in those codes (the
// internal/service/policy/apierror kinds, issue #912; "other" is a kind the
// Policy Engine could not place, a kind the query does not know is counted as
// plain "api_error").
var apiErrorKinds = map[string]string{
	"timeout":          "時間切れ",
	"network":          "通信できない",
	"http_5xx":         "サーバーエラー（5xx）",
	"http_429":         "呼び出しが多すぎる（429）",
	"http_4xx_other":   "リクエストが拒否された（4xx）",
	"invalid_response": "応答が不正",
	"persist_failed":   "判断を保存できない",
	rejectCodeOther:    "その他",
}

// PolicyRejectCode is the name of a code of risk_rejections'
// other_reject_breakdown: a Policy Engine reject head ("jev_none"), a
// meta_skip head with its reason ("meta_skip:no_active_model"), api_error with
// its kind ("api_error:timeout"), "unknown"
// (no reason recorded) or "other" (a reason the query does not know).
func PolicyRejectCode(code string) string {
	if code == rejectCodeOther {
		return "その他の理由"
	}
	head, sub, hasSub := strings.Cut(code, ":")
	name := RejectReason(head)
	if !hasSub {
		return name
	}
	if head == "api_error" {
		return name + "（" + lookup("api_error_kind", apiErrorKinds, sub, Other) + "）"
	}
	return name + "（" + lookup("meta_skip_reason", metaSkipReasons, sub, Other) + "）"
}
