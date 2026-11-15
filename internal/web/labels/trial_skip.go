package labels

// TrialSkipReasons names jev_trial_skips.reason (domain/jevtrial.SkipReasons):
// why the model-comparison trial did not ask an arm about a live decision.
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
}

// TrialSkipReason is the name of a trial skip reason.
func TrialSkipReason(code string) string {
	return lookup("trial_skip_reason", TrialSkipReasons, code, Unknown)
}
