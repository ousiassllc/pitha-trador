package trenddiag

const notice = "研究用の読み取り専用の推移です。採用・昇格の基準ではなく、しきい値・合否も当てていません。" +
	"夜間ジョブが JST の日が終わった後に記録した jev_evaluation_daily だけを読み、Jev・Risk・Policy・注文・メタモデルの学習には使われず、何も書き込みません"

// definitions names what every field means, in the output itself.
func definitions() map[string]string {
	return map[string]string{
		"record": "jev_evaluation_daily の 1 行 = ある JST 日の 1 つの (model_id, question_version) の、1 つのホライズンでの評価。" +
			"その日の trader LONG/SHORT の全判断（trade_signals・Risk では絞らない）を、記録時のラベル設定（calibration.barrier_*・cost.*）で jev_evaluation と同じ式でラベルして集計したもの。書いた日の値のままで、あとから書き換えない",
		"series":    "(model_id, question_version, horizon_minutes) ごとの別系列。版・モデル・ホライズンが変わると別の系列になる（数え直し）",
		"decisions": "その日の trader LONG/SHORT の判断数（除外前）",
		"evaluated": "llm・買い・売りのラベルがすべて計算でき、判断の足がある判断の数（全アームに共通の比較の行）",
		"excluded":  "decisions − evaluated。約定できる気配なし・窓がホライズンに届かない・足なしに加え、学習 cutoff（metalabel.llm_cutoff）より前の判断の除外を含む",
		"entered":   "evaluated のうち、trade_signals に risk_passed=1 の行がある判断の数（約定したかどうかではない）",
		"win_rate":  "evaluated のうち llm の net_return_bps > 0 の割合（見送りの 0 bps は勝ちに数えない）",
		"mean_net_bps": "その日の合計 net bps（コスト控除後）/ evaluated。llm はその判断自身の方向、random は買いと売りのラベルの平均（期待値。乱数を使わない）、" +
			"momentum は判断の足の return_5m が正なら買い・負なら売り・0 または NULL なら見送り、contrarian はその逆。evaluated が 0 の日は null",
		"llm_minus_random_mean_bps": "同じ行ごとの llm − random の平均（その日の合計の差 / evaluated）",
		"label_settings":            "その日を記録したときの take_profit_bps・stop_loss_bps・barrier_cost_bps とホライズン（分）",
		"label_settings_changed":    "連続する記録日（その系列の隣り合う行）で label_settings のどれかが違えば true。label_settings_changed_on は前の記録日と違った日",
		"cumulative": "期間内の日別行を日クラスタにした累積。llm は evaluated 件数での平均 net bps・勝率・日クラスタのブートストラップの 95% 区間・検出できる最小の差（alpha=0.05 両側・検出力 0.8）と +3 / +5 bps までの追加日数、" +
			"llm_minus_random は日ごとの差の合計を日クラスタにした平均差と区間。evaluated が 0 の日は含めない。jev_evaluation と同じ計算（evaldiag.SummarizeDays）",
		"recording": "記録の状態。first_recorded_day / last_recorded_day / days_recorded は期間に関係なく表全体、enabled と retention_days はいまの設定（research.eval.enabled と research.eval.retention_days のこの 2 つだけを読む）",
	}
}

// caveats are the limits of the numbers, always listed.
func caveats(rec Recording) []string {
	out := []string{
		"記録は夜間ジョブが動いた日だけ。ジョブがオフの間・記録前の日・保持期間を過ぎた日は行がなく、日が飛ぶ（取引日でも行がないことがある）",
		"記録は書いた日のラベル設定・cutoff・データで作った値のまま。設定を変えた後の日とは label_settings が違い、cumulative は異なるラベルの日を混ぜる（label_settings_changed を見る）",
		"検出できる最小の差などは alpha=0.05 両側・検出力 0.8 の固定値での記述的な数字で、採用のしきい値ではない",
		"日クラスタの区間は日数が少ないと出ない（2 日未満は null）。同じ日の判断は相関するため区間は楽観的になりうる",
		"random は期待値（買いと売りの平均）で、実際のランダム抽選の結果ではない",
	}
	if !rec.Enabled {
		out = append(out, "記録のスイッチ（research.eval.enabled）がオフです。新しい日は記録されません（すでにある行は保持期間まで残ります）")
	}
	return out
}
