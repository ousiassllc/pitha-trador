package settings

import "github.com/ousiassllc/pitha-trador/internal/config/watchsettings"

// watchGroup holds the kabu ranking watch size and the observation-only
// symbols with the daily Jev cost cap of their calls (issue #1055). Nothing
// here raises the information API limiter (8 requests/second, official 10),
// which is not a setting, and nothing here changes what is traded.
var watchGroup = opsGroup{
	id:          "ranking-watch",
	name:        "監視銘柄数と観察用銘柄（kabu ランキング監視）",
	description: "kabu のランキング監視（既定）が PUSH 登録・市況取得・Fast Screener の対象にする銘柄数（監視リストの大きさ）と、取引には乗せずデータだけ集める観察用銘柄です。",
	note: "監視銘柄数の上限は 45 件です。kabuステーションの API 登録枠 50 件のうち 5 件を REST の /board・/symbol 用に空けるためで、これを超える値は保存できません（既定も 45 件で、今の動作のままです）。" +
		"立花証券は別の設定（監視銘柄ソース）で、EVENT の購読上限 120 件までです。" +
		"情報 API のレート制限（既定 8 件/秒、公式上限 10 件/秒）は、この設定では外れず、件数を変えても有効です。429 や 4001006 が増えたときは、監視銘柄数を下げて戻せます（保存すると次のサイクル（1 分以内）から反映されます）。" +
		"観察用銘柄は既定オフです。オンにすると、リストの銘柄が held（建玉・注文中）の次の枠に入り（監視銘柄数の枠の中で、ランキング銘柄より先に埋まります）、市況データの取得と Jev Scout の呼び出しだけを行います。" +
		"Jev Trader・Policy のエントリー・発注・Paper・Live には一切乗せません（ランキングに入っても同様です）。" +
		"観察用銘柄の Scout 呼び出しは 1 日の費用上限（JST の日ごと、概算）に達するか、失敗が 20 回に達するとその日は止まります。上限が止めるのは観察用の呼び出しだけで、通常の Scout・Trader は止まりません。Jev シャドーの上限（0.10 USD/日）とは別の枠です。" +
		"Paper 実績 MCP からは変更できません。環境変数では設定できません。",
	fields: []opsField{
		{key: watchsettings.KeyMaxWatched, label: "監視銘柄数（ランキング監視のリストの大きさ）", placeholder: "45",
			hint: "1〜45 の整数です。既定は 45 件です。kabu の登録枠（50 件から REST 用の 5 件を除いた 45 件）を超える値は保存できません。"},
		{key: watchsettings.KeyObservationEnabled, label: "観察用銘柄", onOff: true, hint: "オンで有効、オフで無効です。既定はオフです。"},
		{key: watchsettings.KeyObservationSymbols, label: "観察用銘柄のコード", placeholder: "例: 7203,6758",
			hint: "カンマ区切りで最大 10 件です。スイッチがオンのときだけ有効です。このリストの銘柄は取引に乗りません。"},
		{key: watchsettings.KeyObservationDailyCostCapUSD, label: "観察用 Jev 呼び出しの 1 日の費用上限（USD）", placeholder: "0.10",
			hint: "0 より大きく 100 以下です。既定は 0.10 USD/日です。費用は入力トークン数と公表単価からの概算で、請求額ではありません。"},
	},
}
