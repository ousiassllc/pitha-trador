package settings

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

const researchTachibanaNote = "kabuステーションを証券会社に選んでいるときだけ動きます（立花を選んでいる間は何もしません。同じ認証IDで同時に 2 つのセッションを持てないためです）。" +
	"認証ID（秘密鍵のパスを含む）が未設定なら立花へ何も要求せず、「未設定」と記録するだけです。記録は研究専用の表に書くだけで、取引の判断・Risk・発注・Paper ガード・Live・Jev・監視銘柄には一切使いません。" +
	"保存した値は、次の確認（毎分）から再起動なしで反映されます。環境変数では設定できません。"

// researchTachibanaGroups are the 立花 research-only collections (issues #1082,
// #1083): both are off by default.
var researchTachibanaGroups = []opsGroup{
	{
		id:          "research-tachibana-bars",
		name:        "立花 研究用の日足収集",
		description: "証券会社が kabu のままでも、立花の日足を夜間に daily_bars へ記録し、過去分（最大 20 年）を銘柄ごとに 1 回で埋め戻します。取得速度・開始時刻・市場区分・除外銘柄は「立花 夜間日足バッチ」の設定を使います。",
		note:        researchTachibanaNote + "日中（08:00〜15:30）と立花がログインできない 03:30〜05:30 は動きません。埋め戻しが途中で止まっても、取得済みの銘柄は取り直さず続きから進みます。",
		fields: []opsField{
			{key: tachibanasource.KeyResearchBarsEnabled, label: "研究用の日足収集", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
		},
	},
	{
		id:          "research-tachibana-quotes",
		name:        "立花 研究用の時価記録",
		description: "kabu で見ている銘柄に、手動指定または前日の日足スクリーニング上位を足して（合計 120 銘柄まで）、立花の日中の時価を 1 分ごとに研究用の別テーブルへ記録します。kabu の動きと market_snapshots は変えません。",
		note:        researchTachibanaNote + "接続回数と REST 時価の間隔は下の上限を超えません。保持期間より古い日は、ローカルへ退避してから削除します。",
		fields: []opsField{
			{key: tachibanasource.KeyResearchQuotesEnabled, label: "研究用の時価記録", onOff: true, hint: "オンで有効、オフで無効です。既定はオフ（無効）です。"},
			{
				key:     tachibanasource.KeyResearchQuotesExtraSource,
				label:   "追加する銘柄の出どころ",
				hint:    "manual: 下の「追加銘柄」を使う（既定） / daily_screen: 前日の日足スクリーニング（「立花 スクリーニング」の設定）の上位を使う。",
				options: []string{tachibanasource.ResearchExtraManual, tachibanasource.ResearchExtraDailyScreen},
			},
			{key: tachibanasource.KeyResearchQuotesExtraSymbols, label: "追加銘柄（manual 用）", hint: "kabu で見ている銘柄に足す銘柄です。" + symbolListHint, placeholder: "例: 7203, 6758"},
			{key: tachibanasource.KeyResearchQuotesMaxSymbols, label: "記録する銘柄の上限", hint: fmt.Sprintf("kabu の銘柄と追加銘柄の合計の上限です。1〜%d の整数で、既定は %d です。", tachibanasource.MaxTachibanaWatchSymbols, tachibanasource.DefaultResearchQuotesMaxSymbols)},
			{key: tachibanasource.KeyResearchQuotesMaxConnects, label: "EVENT 接続・切断の 1 日の予算（回）", hint: fmt.Sprintf("%d〜%d の整数です。既定は %d 回で、使い切ると購読の入替を止めます。", tachibanasource.TachibanaEventMaxConnectsMin, tachibanasource.TachibanaEventMaxConnectsMax, tachibanasource.DefaultResearchQuotesMaxConnects)},
			{key: tachibanasource.KeyResearchQuotesRestMinInterval, label: "REST 時価補助: 最短間隔（秒）", hint: fmt.Sprintf("%d〜%d の整数です。既定は %d 秒（5 分に 1 回以下）です。", tachibanasource.TachibanaRestQuoteMinIntervalMin, tachibanasource.TachibanaRestQuoteMinIntervalMax, tachibanasource.DefaultResearchQuotesRestMinInt)},
			{key: tachibanasource.KeyResearchQuotesRetentionDays, label: "保持期間（日）", hint: fmt.Sprintf("%d〜%d の整数です。この日数（JST の日）より古い行は、ローカルへ退避して検証できた日だけ削除します。既定は %d 日です。", tachibanasource.ResearchQuotesRetentionDaysMin, tachibanasource.ResearchQuotesRetentionDaysMax, tachibanasource.DefaultResearchQuotesRetentionDays)},
		},
	},
}
