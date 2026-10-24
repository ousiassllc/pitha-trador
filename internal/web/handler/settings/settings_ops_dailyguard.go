package settings

import "github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"

// dailyGuardGroup holds the day-trading entry-halt thresholds. The yen
// thresholds are derived from an operating capital and percentages; the
// direct-yen rows are kept for installs that
// stored them earlier. Every threshold is off at 0 (or while unset).
var dailyGuardGroup = opsGroup{
	id:   "daily-guard",
	name: "1日の損失上限・利益確保",
	description: "当日の損益や取引回数が下記に達したら、その日は新規エントリーを止めます（保有中のポジションの決済は通常どおり行います。Kill Switch のような強制決済はしません）。" +
		"損失上限と利益確保の発動額は、運用資金に対する % で指定します（円は「運用資金 × %」で自動計算され、運用資金を変えると連動します。運用資金は「Paper の運用資金・建玉上限・1回の最大損失」で、Paper の運用資金と並べて設定します）。0 または未設定でそのルールは無効です。",
	note: "Paper の初期値（運用資金 約50万円・損失 1%・発動 0.5%・戻し 50%・最大 20 回）は目安であり、実際の運用資金と許容できる損失を確認して見直してください。Live の前には必ず見直しが必要です。" +
		"日次ルールの運用資金は、Paper の運用資金（日次損失率の分母とポジションサイズ計算の基準）とは別の値ですが、同じ画面（「Paper の運用資金・建玉上限・1回の最大損失」）で設定でき、異なると警告が出ます。" +
		"保存した値は再起動不要で、次のリスク判定（候補ごと、および 1 分周期のチェック）から反映されます。" +
		"当日の損益は、約定済みの実現損益と保有中ポジションの含み損益の合計（JST の当日分）です。" +
		"停止した理由はホームの「要対応」・通知・ログに表示され、翌営業日に自動で解除されます。" +
		"いったん停止した日は、しきい値を緩めても取り消されません（そのルールを 0 にすると解除されます）。" +
		"risk.yaml の日次損失率（Kill Switch で強制決済する日次損失率。設定キー: max_daily_loss_pct）とは別のルールで、併用できます。",
	fields: []opsField{
		{key: dailyguardsettings.KeyLossLimitPct, label: "1日の損失上限（運用資金に対する %）", placeholder: "0（無効）",
			hint: "当日の損益が -(運用資金 × この%) 以下になったら新規エントリーを止めます（例: 1 なら運用資金 50万円で 5,000 円）。0〜100 で、0 で無効です。円の直接指定と両方あるときは、運用資金が設定されていればこちらが優先されます。"},
		{key: dailyguardsettings.KeyProfitLockTriggerPct, label: "利益確保の発動（運用資金に対する %）", placeholder: "0（無効）",
			hint: "当日の損益の最高値が 運用資金 × この% に達すると、利益確保が有効になります（例: 0.5 なら運用資金 50万円で 2,500 円）。許容戻し率とあわせて設定してください（どちらかが 0 なら無効）。0〜100 で、円の直接指定と両方あるときは、運用資金が設定されていればこちらが優先されます。"},
		{key: dailyguardsettings.KeyProfitLockGivebackPct, label: "利益確保の許容戻し率（%）", placeholder: "0（無効）",
			hint: "当日の最高損益から、この割合以上を失った時点で新規エントリーを止めます（例: 最高 +10,000 円・30 なら +7,000 円以下）。0 より大きく 100 以下です。0 で無効です。"},
		{key: dailyguardsettings.KeyMaxTrades, label: "1日の最大取引回数", placeholder: "0（無効）",
			hint: "当日に新規エントリーした回数（建玉数）がこの回数に達したら新規エントリーを止めます。1 以上の整数で、0 で無効です。"},
		{key: dailyguardsettings.KeyLossLimitJPY, label: "1日の損失上限（円で直接指定）", placeholder: "0（無効）",
			hint: "円で直接指定する従来の設定で、既存の環境の互換のために残しています。当日の損益が -この金額 以下になったら新規エントリーを止めます（例: 50000）。0 で無効です。運用資金と損失上限の % が両方設定されている場合は、% が優先され、この値は使われません。"},
		{key: dailyguardsettings.KeyProfitLockTriggerJPY, label: "利益確保の発動額（円で直接指定）", placeholder: "0（無効）",
			hint: "円で直接指定する従来の設定で、既存の環境の互換のために残しています。当日の損益の最高値がこの金額に達すると、利益確保が有効になります。0 で無効です。運用資金と発動の % が両方設定されている場合は、% が優先され、この値は使われません。"},
	},
}
