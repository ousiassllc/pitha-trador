package settings

import "github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"

// dailyGuardGroup holds the day-trading entry-halt thresholds (issue #753,
// FR-RISK-8). Every threshold is off at 0 (or while unset).
var dailyGuardGroup = opsGroup{
	id:          "daily-guard",
	name:        "1日の損失上限・利益確保",
	description: "当日の損益や取引回数が下記に達したら、その日は新規エントリーを止めます（保有中のポジションの決済は通常どおり行います。Kill Switch のような強制決済はしません）。0 または未設定でそのルールは無効です。",
	note: "保存した値は再起動不要で、次のリスク判定（候補ごと、および 1 分周期のチェック）から反映されます。" +
		"当日の損益は、約定済みの実現損益と保有中ポジションの含み損益の合計（JST の当日分）です。" +
		"停止した理由は全ページ上部の帯・通知・ログに表示され、翌営業日に自動で解除されます。" +
		"いったん停止した日は、しきい値を緩めても取り消されません（そのルールを 0 にすると解除されます）。" +
		"risk.yaml の max_daily_loss_pct（Kill Switch で強制決済する日次損失率）とは別のルールで、併用できます。",
	fields: []opsField{
		{key: dailyguardsettings.KeyLossLimitJPY, label: "1日の損失上限（円）", placeholder: "0（無効）",
			hint: "当日の損益が -この金額 以下になったら新規エントリーを止めます。正の金額（例: 50000）で、0 で無効です。"},
		{key: dailyguardsettings.KeyProfitLockTriggerJPY, label: "利益確保の発動額（円）", placeholder: "0（無効）",
			hint: "当日の損益の最高値がこの金額に達すると、利益確保が有効になります。許容戻し率とあわせて設定してください（どちらかが 0 なら無効）。"},
		{key: dailyguardsettings.KeyProfitLockGivebackPct, label: "利益確保の許容戻し率（%）", placeholder: "0（無効）",
			hint: "当日の最高損益から、この割合以上を失った時点で新規エントリーを止めます（例: 最高 +10,000 円・30 なら +7,000 円以下）。0 より大きく 100 以下です。"},
		{key: dailyguardsettings.KeyMaxTrades, label: "1日の最大取引回数", placeholder: "0（無効）",
			hint: "当日に新規エントリーした回数（建玉数）がこの回数に達したら新規エントリーを止めます。1 以上の整数で、0 で無効です。"},
	},
}
