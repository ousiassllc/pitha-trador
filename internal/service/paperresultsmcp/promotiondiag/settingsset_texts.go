package promotiondiag

const (
	// evidenceNote is why a settings-set result is never eligible_as_evidence: the unpaired periods are not the same rows.
	evidenceNote = "experiment / setting の結果は、前後が別の期間・別の取引で、同じ行ごとの差を取れません（same_rows_and_period などは not_applicable）。" +
		"適用できた項目が全て pass でも、同じ判断の行で比べた候補と同じ強さの根拠にはならないので、eligible_as_evidence は常に false です。" +
		"項目ごとの数字は、人間が提案を読むための参考です。前後は別の期間で、設定を変えたことが原因だという証明にもなりません"
	noExperimentNote = "この設定キーを変える調整の実験の記録がありません。experiment_create で実験を記録し、前後の取引がたまってから呼び直してください。全項目 unknown です"
)

func settingsSetDefinitions() map[string]string {
	return map[string]string{
		"same_rows_and_period":  "適用外（not_applicable）。前後は別の期間・別の取引で、同じ行がない",
		"common_rows":           "前後の取引件数のうち少ない方 >= min_common_rows（クローズ済み取引。実験自身の最低件数 64 より厳しい基準をそのまま使う）",
		"common_days":           "前後それぞれの取引がある JST 日数（クローズ日）のうち少ない方 >= min_days",
		"diff_ci_lower":         "1 取引あたりの純損益（コスト控除後・円）の平均の差（後−前）の Welch 95% 区間の下端 > 0。experiment_compare と同じ区間。単位が円なので、min_diff_ci_lower_bps を 0 より厳しくした場合は unknown（比べる換算がない）。実験の判定が出ていなければ unknown",
		"multiple_comparison":   "差の Welch の両側 p に Holm 補正（記録された全実験＋family_size_extra を 1 つの族とし、他は p=1 とみなす。この実験の p が最小として p×族の大きさ）をかけた値 <= max_adjusted_p。実験の判定が出ていなければ unknown",
		"positive_days":         "後の期間の取引がある日のうち、その日の純損益（円）が前の期間の 1 日あたり平均（前の合計 ÷ 前の取引がある日数）を超えた日の割合（%）>= min_positive_day_pct。実験の判定が出ていなければ unknown",
		"not_one_day_wins":      "後の合計から最も良かった 1 日を除いた額 − 前の合計（円）> 0。実験の判定が出ていなければ unknown",
		"max_drawdown":          "後の期間の最大ドローダウン（円。取引をクローズ順に並べた累積のピークからの落ち込み）<= 前の期間のもの。実験の判定が出ていなければ unknown",
		"after_ai_cost":         "適用外（not_applicable）。取引の損益に AI の費用が紐づいていない",
		"after_training_cutoff": "適用外（not_applicable）。取引は LLM の学習の cutoff で選別されていない",
		"model_version_stable":  "実験の変数以外の Jev のモデル・プロンプトの版が前後の期間と境目で変わっていない（experiment_compare の confounders に model / prompt が無い）。設定・方針の提案が変わった場合は、実験の判定が出ず上の項目が unknown になる",
		"unknown":               "unknown は pass ではない。判定に必要な入力・データが無いことを示し、fail ではない。not_applicable は、その種類の候補には成り立たない項目で、first_unmet には出ない",
	}
}
