package labels

// TrialSkipReasons names jev_trial_skips.reason (domain/jevtrial.SkipReasons):
// why the model-comparison trial did not ask an arm about a live decision.
//
// #nosec G101 -- skip reason codes ("daily_cap_tokens"), not credentials
var TrialSkipReasons = map[string]string{
	"concurrency_full":            "同時に呼べる数がいっぱいだった",
	"min_interval":                "前の呼び出しから間が短かった",
	"daily_cap_overall":           "全体の 1 日の費用上限に達していた",
	"daily_cap_arm":               "アームの 1 日の費用上限に達していた",
	"key_not_set":                 "API キーが未設定だった",
	"prompt_version_unregistered": "プロンプトの版が未登録だった",
	"stopped_payment_required":    "支払いエラーで 1 日停止していた",
	"stopped_forbidden":           "拒否されて 1 日停止していた",
	"stopped_failure_cap":         "失敗が上限に達して 1 日停止していた",
	"stopped_other":               "ほかの理由で停止していた",
	"throttled":                   "混雑で待機中だった",
	"check_failed":                "上限や停止の確認ができなかった",
	"provider_disabled":           "ChatGPT サブスク（Codex）のスイッチがオフだった",
	"provider_busy":               "前の判断の Codex がまだ動いていた",
	"not_logged_in":               "Codex が ChatGPT でログインしていなかった",
	"stopped_usage_limit":         "ChatGPT の使用量の上限で停止していた",
	"stopped_provider_failed":     "Codex の失敗で停止していた",
	"daily_cap_calls":             "Codex の 1 日の呼び出し回数の上限に達していた",
	"daily_cap_tokens":            "Codex の 1 日のトークン数の上限に達していた",
}

// TrialSkipReason is the name of a trial skip reason.
func TrialSkipReason(code string) string {
	return lookup("trial_skip_reason", TrialSkipReasons, code, Unknown)
}
