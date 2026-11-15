package evaldiag

import (
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

const (
	notice = "研究用の読み取り専用の集計です。採用・昇格の基準ではなく、しきい値も当てていません。検出できる最小の差などの数字は、いまの件数でどこまで言えるかの目安で、合否ではありません。" +
		"Jev・Risk・Policy・注文・メタモデルの学習には使われず、何も書き込みません"
	settingsText = "全アームにいまの runtime_settings（calibration.barrier_*・cost.*。未設定はデフォルト）で付け直したラベルです。保存済みラベルは作成時の設定で作られているため、設定を変えていれば stored_label と差が出ます"
	// statisticsText describes the interval and the detectable minimum.
	statisticsText = "日クラスタのブートストラップ（判断の JST 日を単位に復元抽出）の百分位区間。検出できる最小の差 = (z(1-alpha/2) + z(power)) × ブートストラップの標準誤差"
)

// definitions names what every arm and field means, in the output itself.
func definitions() map[string]string {
	return map[string]string{
		"population": "期間内の decision_type=trader かつ direction が LONG/SHORT の全判断。trade_signals・Risk・entry_quality では絞らない。direction=NONE は含まない",
		"label": "calibration.LabelWindow（既存ラベラーと同じ関数）: 判断の応答時刻以降 3 分以内の最初の executable な気配（買いは ask、売りは bid）をエントリー価格に、" +
			"horizon_minutes の間、利確（+take_profit_bps）・損切り（-stop_loss_bps）・タイムアウト（最後の足の終値からコストを引いた値）の先に当たったものを net_return_bps（コスト控除後）とする",
		"llm":                "その判断自身の方向（LONG/SHORT）のラベル",
		"long_leg":           "同じ判断の時刻・銘柄で常に買ったときのラベル",
		"short_leg":          "同じ判断の時刻・銘柄で常に売ったときのラベル",
		"momentum":           "判断の足（同じ銘柄・同じ時刻の market_snapshots）の return_5m が正なら買い、負なら売り、0 または NULL なら見送り（0 bps）",
		"contrarian":         "momentum の逆（return_5m が正なら売り、負なら買い、0 または NULL なら見送り）",
		"none":               "常に見送り。取引しないので net は 0 bps",
		"random":             "方向をランダムに選んだときの期待値: 買いのラベルと売りのラベルの平均（乱数は使わない。決定的）",
		"comparison_set":     "llm・買い・売りの 3 つのラベルがすべて計算でき、判断の足の行がある判断だけを全アームで共通に使う。使えない判断は excluded_by_reason に理由別の件数",
		"direction_split":    "all / long / short は llm の方向（判断の方向）で分ける。ベースラインも同じ行の集合で集計する",
		"win_rate":           "net_return_bps > 0 の件数 / n（見送りの 0 bps は勝ちに数えない）",
		"llm_minus_baseline": "同じ行ごとの llm の net から ベースラインの net を引いた差。日ごとの差の合計を日クラスタとして区間を作る",
		"days_to_detect": "target_bps（+3 / +5）の差（アーム単体では平均）をいまの標準誤差から検出できるまでの追加の JST 営業日数 = max(0, ceil(日数 × (検出できる最小値 / target)^2 − 日数))。" +
			"標準誤差が日数の平方根に反比例して縮む前提。already_detectable=true は今の件数で検出できる。null は標準誤差が未定義（2 日未満）",
		"cohort": "(model_id, question_version) ごとの別集計。版が変わると数え直し",
		"stored_label": "calibration_outcomes の label_version=2・horizon_minutes が一致する保存済みの net_return_bps（Calibration 画面と同じ値）。" +
			"all_decisions は期間内の全 LONG/SHORT trader 判断、evaluated_rows は比較に使った行。recomputed_mean_on_labeled_rows は保存ラベルのある評価行の付け直した値の平均",
		"llm_by_entry": "llm アームを、trade_signals に risk_passed=1 の行がある判断（entered）とそれ以外（skipped）に分けたもの",
	}
}

// caveats are the limits of the numbers, always listed.
func caveats(cutoff Cutoff) []string {
	out := []string{
		"ラベルはいまの設定で付け直したもの。保存済みラベルと値が違う場合は stored_label.max_abs_difference_bps を見る",
		"検出できる最小の差は alpha=0.05 両側・検出力 0.8 の固定値での記述的な数字で、採用のしきい値ではない",
		"件数が同じ日に偏る（同じ銘柄・同じ相場の判断が重なる）ため、日クラスタの区間でも楽観的になりうる。日数が少ないと区間は出ない",
		"random は期待値（買いと売りの平均）で、実際のランダム抽選の結果ではない",
		"entered / skipped は trade_signals の risk_passed=1 の有無で、約定したかどうかではない",
	}
	if !cutoff.Applied {
		out = append(out, "metalabel.llm_cutoff が仮置き（"+metalabelsettings.DefaultLLMCutoff+"）のため、学習データに含まれうる日も除外していない。LLM がその期間の値動きを知っていた可能性がある（llm_cutoff.applied=false）")
	}
	return out
}
