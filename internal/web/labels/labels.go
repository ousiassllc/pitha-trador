// Package labels is the one place that names the internal codes the screens
// would otherwise print (exit reasons, Kill Switch reasons, order proposal
// reason codes, activity types, ...) in plain Japanese (issue #904).
//
// The screens call these functions at the last step of rendering: the code
// itself stays what it is everywhere else (data-* attributes, the API, the MCP,
// the database), only the text an operator reads changes. A code a table does
// not know is never printed: the reader gets Unknown and the code goes to the
// log (once per code), so a new code is noticed without leaking into the UI.
//
// The same names are kept for the Lit components in
// static/src/components/lib/labels.ts; TestTypeScriptLabelsMatchGo keeps the
// two in step.
package labels

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// Unknown is what an operator reads in place of a reason code that no table
// below knows.
const Unknown = "その他（理由コードあり）"

// Other is what is shown in place of a name of another kind
// (a queue, a job status, a side, ...) that is not known.
const Other = "その他"

var warned sync.Map // kind + "\x00" + code -> struct{}

// lookup is the name of code in table. A code the table does not know is
// logged once and replaced by fallback.
func lookup(kind string, table map[string]string, code, fallback string) string {
	if name, ok := table[code]; ok {
		return name
	}
	if _, seen := warned.LoadOrStore(kind+"\x00"+code, struct{}{}); !seen {
		slog.Warn("labels: a code without a screen label was replaced", "kind", kind, "code", code)
	}
	return fallback
}

// Numbered is how a record of the application is named on a screen: the noun,
// a space and the number with 番 ("注文提案 5 番"). It is the one way every
// screen writes a record's number: no "#" in front of it.
func Numbered(noun string, id int64) string { return noun + " " + strconv.FormatInt(id, 10) + " 番" }

// cutoffModelKey is the setting the LLM cutoff warning (shared with the API, the
// MCP and the log, so its text is not changed) tells the operator to fill in.
const cutoffModelKey = "metalabel.llm_cutoff_model"

// ScreenWarning is a warning shared with the API / MCP / log as the screen
// reads it: the wording is kept, only the setting key in it is named as the
// Settings screen names that item.
func ScreenWarning(warning string) string {
	return strings.ReplaceAll(warning, cutoffModelKey, "モデル名")
}

// ExitReasons names positions.exit_reason (domain.ExitReason*).
var ExitReasons = map[string]string{
	"stop_loss":                        "損切り",
	"take_profit":                      "利確",
	"trailing_stop":                    "トレーリングストップ",
	"jev_direction_reversed":           "Jev の方向が反転",
	"continuation_probability_dropped": "継続確率が低下",
	"vwap_cross":                       "VWAP をまたいだ",
	"max_holding":                      "最大保有時間に到達",
	"force_flat_before_close":          "大引け前の強制決済",
	"manual":                           "手動で決済",
	"force_close":                      "Kill Switch による強制決済",
	"proposal_close":                   "AIの注文提案による決済",
}

// ExitReason is the name of a position's exit reason.
func ExitReason(code string) string { return lookup("exit_reason", ExitReasons, code, Unknown) }

// KillReasons names kill_switch_events.reason (domain.KillReason*).
var KillReasons = map[string]string{
	"daily_loss_limit":           "日次の損失上限に達した",
	"consecutive_losses":         "連続で損失が出た",
	"market_data_down":           "市況データが止まった",
	"jev_api_down":               "Jev の API が止まった",
	"broker_api_error":           "証券会社の API でエラーが出た",
	"unexpected_position":        "想定外のポジションがあった",
	"fill_discrepancy":           "約定の内容が食い違った",
	"db_write_failure":           "データベースへの書き込みに失敗した",
	"operator_heartbeat_timeout": "画面からの応答が途絶えた",
	"operator_manual":            "手動で発動した",
}

// KillReason is the name of a Kill Switch reason.
func KillReason(code string) string { return lookup("kill_reason", KillReasons, code, Unknown) }

// ResolvedBys names kill_switch_resolutions.resolved_by (domain.ResolvedBy*).
var ResolvedBys = map[string]string{"auto": "自動", "manual": "手動"}

// ResolvedBy is the name of who lifted a Kill Switch.
func ResolvedBy(code string) string { return lookup("resolved_by", ResolvedBys, code, Other) }

// RetrainSkips names the nightly retraining skip codes
// (internal/repository/metalabel Skip*).
var RetrainSkips = map[string]string{
	"too_few_rows_before_oos":  "検証期間より前のラベル付き判断が不足",
	"too_few_rows_after_purge": "重なり除去後の学習用データが不足",
	"training_data_unusable":   "件数はあるが学習できない（ラベルが片側のみ等）",
}

// RetrainSkip is the name of a retraining skip code.
func RetrainSkip(code string) string { return lookup("retrain_skip", RetrainSkips, code, Unknown) }

// RejectReasons names the Policy Engine's reject_reason heads (the constants of
// internal/service/policy, the meta-labeling model's and the Risk Engine's).
var RejectReasons = map[string]string{
	"jev_none":                                 "Jev が方向なしと判断",
	"probability_below_threshold":              "確率がしきい値未満",
	"entry_quality_below_threshold":            "エントリー品質がしきい値未満",
	"continuation_probability_below_threshold": "継続確率がしきい値未満",
	"toxic_flow_above_threshold":               "毒性フローがしきい値超過",
	"liquidity_stressed_above_threshold":       "流動性ストレスがしきい値超過",
	"spread_too_wide":                          "スプレッドが広すぎる",
	"thin_liquidity":                           "板が薄い",
	"special_quote":                            "特別気配",
	"price_limit":                              "値幅制限",
	"not_lendable":                             "貸株なし（売り不可）",
	"missing_data":                             "データ欠損",
	"api_error":                                "API エラー",
	"not_calibrated":                           "較正が未完了",
	"risk_engine_rejected":                     "リスクエンジンが拒否",
	"meta_skip":                                "メタモデルが見送り",
	"meta_model_unavailable":                   "メタモデルを使えない",
	"vwap_z_unavailable":                       "VWAP 乖離を算出できない",
	"vwap_not_extreme":                         "VWAP 乖離が小さい",
	"unknown":                                  "理由の記録なし",
}

// RejectReason is the name of a trade signal's reject_reason head.
func RejectReason(code string) string { return lookup("reject_reason", RejectReasons, code, Unknown) }

// ArmNames names the Paper A/B arms (marketregime.Arm*).
var ArmNames = map[string]string{"momentum": "モメンタム固定", "regime": "レジーム切替"}

// ABArm is the name of an A/B arm.
func ABArm(code string) string { return lookup("ab_arm", ArmNames, code, Other) }

// ModelStatuses names metalabel_models.status.
var ModelStatuses = map[string]string{"active": "採用", "rejected": "不採用"}

// ModelStatus is the name of a meta-labeling model's status.
func ModelStatus(code string) string { return lookup("model_status", ModelStatuses, code, Other) }

// ModelAlgorithms names metalabel_models.algorithm.
var ModelAlgorithms = map[string]string{"logistic_regression": "ロジスティック回帰"}

// ModelAlgorithm is the name of a meta-labeling model's algorithm.
func ModelAlgorithm(code string) string {
	return lookup("model_algorithm", ModelAlgorithms, code, Other)
}
