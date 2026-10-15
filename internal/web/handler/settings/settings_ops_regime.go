package settings

import "github.com/ousiassllc/pitha-trador/internal/config/regimesettings"

// regimeGroup holds the market regime detection and the momentum /
// VWAP-reversion switch (issue #755, parent #751).
var regimeGroup = opsGroup{
	id:          "regime",
	name:        "相場状態（レジーム）判定とモメンタム／VWAP回帰の切替",
	description: "市場指数（market_index）の 1 分足リターンの自己相関とボラティリティから相場状態を「トレンド（trend）／平均回帰（mean_revert）／不明（unknown）」に判定し、判断ごとに保存します。平均回帰のときだけ、寄り付き以降の VWAP からの乖離 z スコアで逆張りする VWAP 回帰モードに切り替えます（issue #755、親 #751）。",
	note: "保存した値は再起動不要で、次の判断から反映されます。paper のみで、live には適用されません。" +
		"A/B 比較を有効にすると、判断ごとに「モメンタム固定（momentum）」と「レジーム切替（regime）」の 2 つのアームのどちらかに自動で振り分けます。" +
		"両アームの違いが出るのは平均回帰レジームだけで、トレンド・不明のときはどちらも従来のモメンタム追随です。" +
		"アーム別のコスト控除後 EV・勝率はキャリブレーション画面で確認できます。無効のときは全判断がモメンタム固定です。" +
		"エントリーの可否はメタラベリングモデルの判定を通ります。環境変数では設定できません。",
	fields: []opsField{
		{key: regimesettings.KeyABEnabled, label: "A/B 比較（モメンタム固定 vs レジーム切替）", placeholder: "0（無効）",
			hint: "1 で有効、0 で無効です。既定は 0（無効＝全判断がモメンタム固定）です。"},
		{key: regimesettings.KeyWindowMinutes, label: "リターンの窓幅（分）",
			hint: "市場指数の 1 分足リターンを何分遡って自己相関・ボラティリティを測るかです。5〜120 の整数で、既定は 30 分です。"},
		{key: regimesettings.KeyAutocorrThreshold, label: "自己相関のしきい値",
			hint: "リターンの 1 次自己相関がこの値以上ならトレンド、−この値以下なら平均回帰、その間は不明です。0 より大きく 1 より小さい値で、既定は 0.2 です。"},
		{key: regimesettings.KeyMinVolatilityBps, label: "ボラティリティの下限（bps）",
			hint: "1 分足リターンの標準偏差がこの値（1 bps = 0.01%）を下回る静かな相場は、自己相関が意味を持たないため不明とします。0 以上 1000 以下で、既定は 0.5 bps です。"},
		{key: regimesettings.KeyVWAPZThreshold, label: "VWAP 乖離 z スコアのしきい値",
			hint: "VWAP 回帰モードで、価格が VWAP から寄り付き以降の典型的な乖離のこの倍数以上離れたら、VWAP への戻りを狙って逆張りします。0 より大きく 10 以下で、既定は 2 です。"},
	},
}
