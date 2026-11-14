package researchdiag

import rlabel "github.com/ousiassllc/pitha-trador/internal/domain/researchlabel"

// definitions say what each number is, in the answer itself.
func definitions() Definitions {
	return Definitions{
		Entry: "エントリーはそのスナップショットの足: 買いは ask、売りは bid で入る（その気配が無い側は集計に入らない）。" +
			"gross = (出口価格 - 入口) / 入口 × 10000 × 方向（売りは符号反転）の bps で、calibration の grossReturnBps と同じ。出口価格は出口の足の price",
		Exit: map[string]string{
			rlabel.Horizon30m: "エントリーの足の時刻 + 30 分以前の最後の足で、目標時刻との差が 2 分（calibration.HorizonBarTolerance）以内のとき。昼休み・引け・欠測で足りない窓は行を作らない（短い窓は記録しない。ライブのラベルと同じ規則）",
			rlabel.Horizon60m: "同上（60 分）",
			rlabel.HorizonClose: "同じ JST 日の最後の足（15:30 以前）で、その時刻が 15:25 JST 以降のとき。15:25 以降の足には close の行が無い（保有するものが残らない）。" +
				"出口は大引けの足の price で、引けの板のスプレッドは含まない",
		},
		Cost: "コストは記録時の設定でライブのラベルが置く値: costsettings.LabelBarriers（calibration.barrier_cost_bps を cost.* で動かした値）の買い・売り別。" +
			"ラベル行に設定の写しは無いので、行ごとの long_cost_bps / short_cost_bps が記録時の値（設定を変えた後は日ごとに違いうる）",
		Net: "net = gross - cost（行ごと）。勝率は net > 0 の行の割合",
		Spread: "spread_bps はエントリーの足の market_snapshots.spread_bps（NULL の行は cost_vs_spread の平均・中央値・割合に入らない）。" +
			"net_after_spread_bps = gross - spread_bps / 2 の平均: エントリーの気配（買いは ask、売りは bid）に入口側の半スプレッドはすでに含まれるので、" +
			"出口側の半スプレッドの代わりとして spread_bps / 2 を引いた近似（出口の足の実スプレッドではない）。" +
			"share_spread_above_cost は spread_bps がコスト（その行の assumed cost）を超えた行の割合",
		Scope: map[string]string{
			ScopeSnapshots: "期間内の全スナップショット。買い・売りの両方を返す（行ごとに両側が独立。方向を選んだ結果ではない）",
			ScopeTrader:    "trader 判断（LONG/SHORT。NONE は除く）があった足。判断自身の方向の側だけを返す。判断 1 件が 1 行（同じ足に判断が複数あればその数だけ）",
			ScopeScout:     "scout 判断（LONG/SHORT。NONE は除く）があった足。それ以外は trader と同じ",
		},
		Intervals: "mean_net_ci は JST 日を 1 まとまりに復元抽出した 2000 回のブートストラップ（固定シード）の 95% パーセンタイル区間。" +
			"detectable_minimum_bps = (z(0.975) + z(0.8)) × ブートストラップの標準誤差。固定の α=0.05（両側）・検出力 0.8 での記述で、採用のしきい値ではない。" +
			"日数が 2 未満のとき区間・標準誤差・最小差は null。中央値は、ホライズンの行が 100000 を超えると snapshot_id のハッシュで選んだ k% の標本の値（median_sample_percent が 100 未満のとき。平均・勝率・件数は常に全行）",
	}
}
