package labels

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// rejectCodeOther is the code risk_rejections' other_reject_breakdown gives a
// reason this build does not know (rejectCodeOther in internal/service/paperresultsmcp/query).
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

// jevNoneRegimes are what follows "jev_none:regime=" (the regimes of Jev's
// answer, shown as the domain's own text, issue #922). The query passes no
// other name, so the table has no "other": that is "jev_none:other".
var jevNoneRegimes = map[string]string{
	domain.JevRegimeTrend:    domain.JevRegimeTrend,
	domain.JevRegimeRange:    domain.JevRegimeRange,
	domain.JevRegimeBreakout: domain.JevRegimeBreakout,
	domain.JevRegimeChaotic:  domain.JevRegimeChaotic,
}

// jevNoneConfBands names what follows "jev_none:conf=": the confidence bands
// of Jev's answer (issue #922).
var jevNoneConfBands = map[string]string{
	"lt_0.4":     "0.4 未満",
	"0.4_to_0.6": "0.4 以上 0.6 未満",
	"0.6_to_0.8": "0.6 以上 0.8 未満",
	"ge_0.8":     "0.8 以上",
}

// jevNoneSub names what follows "jev_none:": "other" (a row of the new form
// that is not a known regime or a confidence), "regime=<regime>" or
// "conf=<band>".
func jevNoneSub(sub string) string {
	if sub == rejectCodeOther {
		return "その他"
	}
	if regime, ok := strings.CutPrefix(sub, "regime="); ok {
		return "局面 " + lookup("jev_none_regime", jevNoneRegimes, regime, Other)
	}
	if band, ok := strings.CutPrefix(sub, "conf="); ok {
		return "確信度 " + lookup("jev_none_conf_band", jevNoneConfBands, band, Other)
	}
	return Other
}

// PolicyRejectCode is the name of a code of risk_rejections'
// other_reject_breakdown: a Policy Engine reject head ("jev_none"), a
// meta_skip head with its reason ("meta_skip:no_active_model"), api_error with
// its kind ("api_error:timeout"), jev_none by regime ("jev_none:regime=RANGE")
// or by confidence band ("jev_none:conf=ge_0.8"), "unknown"
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
	switch head {
	case "api_error":
		return name + "（" + lookup("api_error_kind", apiErrorKinds, sub, Other) + "）"
	case "jev_none":
		return name + "（" + jevNoneSub(sub) + "）"
	}
	return name + "（" + lookup("meta_skip_reason", metaSkipReasons, sub, Other) + "）"
}
