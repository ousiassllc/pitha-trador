package slicediag

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

const (
	notice = "研究用の読み取り専用の集計です。採用・昇格の基準ではなく、しきい値も当てていません。バケットの並びは固定で、結果で並べ替えず、最良のバケットも示しません。" +
		"Jev・Risk・Policy・注文・メタモデルの学習には使われず、何も書き込みません"
	exploratoryNote = "exploratory の切り口（weekday・volatility_band・turnover_band・spread_band・sector）は、結果を見たあとに足した切り口です。ここで目立つバケットは検証済みの結果ではなく仮説で、" +
		"別の期間・別のデータで確かめるまで何も言えません。Holm 補正後の p 値も、見た切り口の数を数えているだけで、見ていない切り方までは補正できません"
	preregistrationText = "preregistered の切り口は " + PreregisteredOn + "（issue " + PreregisteredIssue + "）にコードで固定した（regime・time_of_day・direction・confidence_band）。バケットの境界もこの日に固定し、結果を見て動かさない。" +
		"それ以外はすべて exploratory"
	statisticsText = "日クラスタのブートストラップ（判断の JST 日を単位に復元抽出）の百分位区間。検出できる最小の差 = (z(1-alpha/2) + z(power)) × ブートストラップの標準誤差。jev_evaluation と同じ定数"
	correctionText = "Holm-Bonferroni（stats.Holm）。コホートごと・family（preregistered / exploratory）ごとに、p 値のある全バケット（low_sample も含む。dimensions で絞っても family 全体）に適用。all は family に入れない"
)

// definitions names what every dimension, bucket and field means, in the output itself.
func definitions() map[string]string {
	return map[string]string{
		"population":      "jev_evaluation と同じ。期間内の decision_type=trader かつ direction が LONG/SHORT の判断のうち、コホートの比較に使えた行（evaluated）。cutoff・除外理由・ラベルも同じ",
		"label":           "各行の llm アームのコスト控除後の net_return_bps（bps）。jev_evaluation の llm と同じ値",
		"cohort":          "(model_id, question_version) ごとの別集計。バケットも補正もコホートの中で閉じる",
		"all":             "そのコホートの評価行すべての要約（基準）。切り口ではなく、Holm の family に入れない（holm_adjusted_p は null）",
		"dimension_rows":  "各切り口の rows は、その切り口のバケットの n の合計で、コホートの evaluated と一致する（どの行もちょうど 1 つのバケットに入る）",
		"preregistered":   preregistrationText,
		"exploratory":     exploratoryNote,
		DimRegime:         "preregistered。jev_decisions.market_regime（trend / mean_revert）。記録なし・それ以外の値は unknown",
		DimTimeOfDay:      "preregistered。判断の時刻（JST）。open 09:00-09:30（寄り直後）・morning 09:30-11:30（前場）・afternoon 12:30-14:30（後場）・pre_close 14:30-15:30（引け前）・それ以外は other。各区間は開始を含み終了を含まない",
		DimDirection:      "preregistered。判断自身の方向（LONG / SHORT）",
		DimConfidenceBand: "preregistered。jev_decisions.confidence の固定の帯 [0.0,0.6) [0.6,0.7) [0.7,0.8) [0.8,0.9) [0.9,1.0]（下端を含み上端を含まない。最後だけ 1.0 を含む）。記録なし・0〜1 の外は unknown",
		DimWeekday:        "exploratory。判断の JST の曜日 mon〜fri。土日は other",
		DimVolatilityBand: "exploratory。jev_decisions.regime_volatility_bps の三分位（low / mid / high）。コホートの評価行のうち値のある行を値の昇順（同値は判断 id の昇順）に並べ、i 番目（k 件中）を i×3÷k の段に入れる。値がない行は unknown。edges は mid と high の最初の値。同値が境界をまたぐと同じ値が 2 つのバケットに入りうる",
		DimTurnoverBand:   "exploratory。判断の足の turnover_5m の三分位。分け方は volatility_band と同じ",
		DimSpreadBand:     "exploratory。判断の足の spread_bps の三分位。分け方は volatility_band と同じ",
		DimSector:         "exploratory。instruments.sector。コホートの評価行数の多い上位 " + fmt.Sprint(MaxSectorBuckets) + " 業種（同数は名前順）はそれぞれバケット、残りは other、記録なしは unknown。バケットは名前順で、other・unknown は該当行があるときだけ出る",
		"low_sample":      fmt.Sprintf("n < %d または days < %d のバケットの印（low_sample_reasons: %s / %s）。除外はせず、family に入れたまま印だけ付ける", MinBucketRows, MinBucketDays, ReasonRowsBelowMin, ReasonDaysBelowMin),
		"p_value":         "「真の平均が 0」の両側 p 値（ブートストラップの標準誤差による正規近似）。2 日未満は null。検定の合否ではなく、Holm 補正の入力",
		"holm_adjusted_p": correctionText,
		"family_size":     "Holm の m: family の全バケットのうち p 値のあるものの数（dimensions で絞っても同じ）。buckets は p 値のないものも含む family のバケット数",
	}
}

// caveats are the limits of the numbers, always listed.
func caveats(cutoffApplied bool) []string {
	out := []string{
		"ラベルはいまの設定で付け直したもの（jev_evaluation と同じ）。保存済みラベルとの差は jev_evaluation の stored_label を見る",
		"件数・日数が少ないバケットの区間・p 値・検出できる最小の差は不安定で、low_sample の印がある。印のないバケットでも、同じ日・同じ銘柄・同じ相場に判断が偏れば日クラスタの区間でも楽観的になりうる",
		"切り口どうしは独立ではない（同じ行が全切り口に 1 回ずつ入る）。Holm 補正は family の中の検定の数だけを数えており、切り口の組み合わせ（交互作用）や、ここに出ていない切り方は数えていない",
		"三分位の境界はコホートの評価行から計算されるため、期間・フィルタが変わると同じ帯でも中身が変わる。コホートどうし・期間どうしの帯は比べられない",
		"p 値・補正後の p 値・low_sample は記述的な数字で、採用や昇格の判定ではない",
	}
	if !cutoffApplied {
		out = append(out, "metalabel.llm_cutoff が仮置き（"+metalabelsettings.DefaultLLMCutoff+"）のため、学習データに含まれうる日も除外していない。LLM がその期間の値動きを知っていた可能性がある（llm_cutoff_applied=false）")
	}
	return out
}
