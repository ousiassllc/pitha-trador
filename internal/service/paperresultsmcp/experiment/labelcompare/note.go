package labelcompare

// DescriptionNote is appended to experiment_compare's tool description.
const DescriptionNote = "【label_based（取引ゼロでも出る別の判定）】応答の label_based に、取引ではなくラベルで前後を比べた結果を別枠で返す（verdict / judged には混ぜない。上の取引ベースの判定は変わらない）。" +
	"前後それぞれの Jev trader 判断の件数（LONG / SHORT / NONE）、判断が付けた方向の既存のコスト控除後ラベル（calibration_outcomes の net_return_bps。LONG / SHORT / 合算）の平均、" +
	"見送り（trade_signals の拒否）の件数と上位の理由コード、研究用ラベル（research_label_day_stats）のホライズン別の平均 net bps と、それぞれの後−前の差を返す。" +
	"区間は JST の日を 1 まとまりとした日クラスタの 95% 信頼区間（前後の差は日平均どうしの Welch）。days_needed に判定まであと何日か（remaining_days）を返す" +
	"（日ごとのばらつきのまま、区間の半幅が |差| を下回るのに要る前後それぞれの日数 − 観測済みの日数。差がほぼ 0 などで出せないときは null と reason）。" +
	"label_verdict は insufficient_days / not_computable / improved / worsened / inconclusive（交絡・切り替え未確認・中止の実験は取引ベースと同じく判定しない）。" +
	"最小日数は " + MinDaysProvisionalText + "。ラベルは実現損益ではなく、保有の重なるラベルは時間的に重なる。補助の証拠としてだけ使う（caution）"
