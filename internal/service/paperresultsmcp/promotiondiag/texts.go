package promotiondiag

const (
	notice = "昇格の提案のための「証拠」の一覧です。何も適用せず（applied=false）、設定・プロンプト版・モデルを変えず、何も書き込みません。" +
		"eligible_as_evidence は指示ではありません。採用は人間が決めます（あおいの提案と悠太さんの OK）。研究用で、Jev・Risk・Policy・注文・メタモデルの学習には使われません"
	criteriaNote = "各基準の値はフロア（docs/architecture/research-tools/paper-results-mcp-promotion.md）か、Settings の research.promotion.* で厳しくした値か、この呼び出しで厳しくした値。緩める方向には動かせない（フロアより緩い設定値は無視され、緩い呼び出しの値は invalid_argument）"
)

func definitions() map[string]string {
	return map[string]string{
		"same_rows_and_period":  "共通の行（候補といまのものの両方が答えた行）が、期間内の基準（ライブ）の行の 80%（MinCoverage）以上",
		"common_rows":           "共通の行数 >= min_common_rows",
		"common_days":           "共通の行が及ぶ JST 日数 >= min_days",
		"diff_ci_lower":         "差（候補−いま。同じ行ごと・コスト控除後ラベル）の日クラスタ 95% 区間の下端 > min_diff_ci_lower_bps",
		"multiple_comparison":   "差の Holm 補正後の p（台帳の全候補＋family_size_extra を 1 つの族として）<= max_adjusted_p",
		"positive_days":         "差の日合計が正の日の割合（%）>= min_positive_day_pct",
		"not_one_day_wins":      "最も良かった 1 日を除いた差の合計（bps）> 0",
		"max_drawdown":          "共通の行を時刻順に並べたときの最大ドローダウン（bps。累積 net のピークからの最大の落ち込み。開始の 0 もピーク）が、候補 <= いまのもの",
		"after_ai_cost":         "AI の費用の差（候補−いま）を bps に換算して引いた後の差の平均 > 0。usd_jpy と notional_jpy が両方無い、または費用が不明の行があれば unknown",
		"after_training_cutoff": "metalabel.llm_cutoff が確認済み（仮置きでない）。仮置きなら unknown（LLM が評価期間の値動きを知っていた可能性を除けない）",
		"model_version_stable":  "期間内の model_id が候補・いまのもの（ライブ）のどちらも 1 種類（途中でモデルが変わっていない）",
		"unknown":               "unknown は pass ではない（first_unmet に出る）。判定に必要な入力・データが無いことを示し、fail ではない",
	}
}
