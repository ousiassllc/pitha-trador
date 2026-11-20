package settings

import "github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"

// researchFXGroup holds the switch and the currency pairs of the research-only
// FX 1-minute candle record (issue #1084).
var researchFXGroup = opsGroup{id: "research-fx", name: "研究用の為替 1 分足の記録",
	description: "GMOコイン 外国為替FX の公開 API から、通貨ペア（既定は USD_JPY）の 1 分足（売値・買値）を取得し、研究専用の表に記録します。取引の判断・Risk・発注・Paper ガード・Live・Jev・Feature Engine には一切使いません。研究用であり、取得した値の再配布はしません。",
	note: "既定はオフ（無効）で、オフの間は外部への要求も書き込みも一切ありません。オンにすると、7:00 JST 以降のメンテナンスチェック（10 分ごと）から、直近 7 日のうち取引日が終わっていて未取得の日を古い順に、通貨ペアごと 1 日ずつ取得します（1 日あたり売値・買値の 2 回の要求で、GMO への要求は 1 秒に 1 回までです）。" +
		"取得に失敗した日は記録せず、次のチェックでやり直します。土日・休日など足が 1 本も無い日は「市場休み」として記録し、欠けとは区別されます。kabu の登録枠は使いません。" +
		"オン・オフは Paper 実績 MCP からも変更できます（通貨ペアは変更できません）。保存した値は再起動不要で、次のチェックから反映されます。環境変数では設定できません。",
	fields: []opsField{
		{key: researchfxsettings.KeyEnabled, label: "研究用の為替 1 分足の記録", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
		{key: researchfxsettings.KeySymbols, label: "通貨ペア", placeholder: "USD_JPY",
			hint: "GMO の通貨ペア名をカンマ区切りで入力します（例: USD_JPY,EUR_USD）。最大 5 件で、重複はできません。既定は USD_JPY です。"},
	}}
