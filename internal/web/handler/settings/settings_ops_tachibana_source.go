package settings

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

const (
	sourceNote        = "保存した値は、次の夜間バッチ・次の監視リスト確定・次回の EVENT 接続から使われます（環境変数は使いません）。"
	symbolListHint    = "銘柄コード（英数字）をカンマまたは空白区切りで入力します。最大 120 件です。「既定に戻す」で空にできます。"
	screenSourceNote  = "各指標は、上位件数だけ候補を選びます。重みは、候補が空き枠を超えたときの優先順位に使います。重み 0 または上位件数 0 の指標は使いません（少なくとも 1 つは残してください）。"
	screenWeightLabel = "重み: "
	screenTopNLabel   = "上位件数: "
)

// screenIndicatorLabels names the screening indicators (kabu `/ranking` 種別に
// 寄せたもの); the keys are tachibanasource.TachibanaScreenIndicators.
var screenIndicatorLabels = map[string]string{
	tachibanasource.ScreenGainRate:      "値上がり率",
	tachibanasource.ScreenLossRate:      "値下がり率",
	tachibanasource.ScreenVolume:        "売買高",
	tachibanasource.ScreenTurnover:      "売買代金",
	tachibanasource.ScreenVolumeSurge:   "出来高急増（過去 20 営業日平均との比）",
	tachibanasource.ScreenTurnoverSurge: "売買代金急増（過去 20 営業日平均との比）",
	tachibanasource.ScreenRangeRate:     "値幅率",
}

// tachibanaSourceGroups are the 立花 監視銘柄ソース groups (issue #728, child
// of #726): candidate source and symbol lists, the nightly daily-bar batch,
// the screening, and the intraday EVENT / REST budgets.
var tachibanaSourceGroups = []opsGroup{
	{
		id:          "tachibana-source",
		name:        "立花 監視銘柄ソース（候補ソース・銘柄リスト）",
		description: "立花証券 e支店API 選択時に、翌営業日の監視銘柄（最大 120 件・EVENT 購読の母集団）をどう決めるかの設定です。kabu 選択時の監視（FR-SCHED-9）には影響しません。",
		note:        sourceNote,
		fields: []opsField{
			{
				key:     tachibanasource.KeyTachibanaCandidateSource,
				label:   "候補ソース",
				hint:    "daily_screen: 前日引け後の日足スクリーニングで決める（既定） / fixed: 下の「固定リスト」をそのまま使う（日足が取れない日の退避先にもなります）。",
				options: []string{tachibanasource.TachibanaSourceDailyScreen, tachibanasource.TachibanaSourceFixed},
			},
			{key: tachibanasource.KeyTachibanaManualSymbols, label: "手動指定銘柄", hint: "daily_screen のとき、スクリーニング結果に加えて必ず監視する銘柄です。" + symbolListHint, placeholder: "例: 7203, 6758"},
			{key: tachibanasource.KeyTachibanaFixedSymbols, label: "固定リスト（fixed 用）", hint: "fixed のとき、スクリーニングをせずそのまま監視リストにする銘柄です。" + symbolListHint, placeholder: "例: 7203, 6758"},
		},
	},
	{
		id:          "tachibana-nightly",
		name:        "立花 夜間日足バッチ",
		description: "引け後に全銘柄の日足を取得してスクリーニングの元データにする夜間バッチの設定です。日中（08:00〜15:30）は全銘柄の巡回も日足の一括取得も行いません。",
		note:        sourceNote,
		fields: []opsField{
			{key: tachibanasource.KeyTachibanaNightlyRunTime, label: "実行開始時刻（JST）", hint: "HH:MM 形式です。既定は 18:00 以降、マニュアル推奨の翌 01:00 以降も選べます。08:00〜15:30 は指定できません。", placeholder: tachibanasource.DefaultTachibanaNightlyRunTime},
			{key: tachibanasource.KeyTachibanaNightlyRatePerSecond, label: "取得速度（件/秒）", hint: fmt.Sprintf("%s〜%s の数値です。既定は 1 件/秒で、1 銘柄ずつ直列に取得します。", numText(tachibanasource.TachibanaNightlyRatePerSecondMin), numText(tachibanasource.TachibanaNightlyRatePerSecondMax))},
			{key: tachibanasource.KeyTachibanaNightlyMarkets, label: "対象の市場区分", hint: "prime / standard / growth / other をカンマ区切りで指定します（銘柄マスタの市場区分と照合します）。", placeholder: tachibanasource.DefaultTachibanaNightlyMarkets},
			{key: tachibanasource.KeyTachibanaNightlyMinPrice, label: "除外: 株価の下限（円）", hint: "前日終値がこの値未満の銘柄を対象外にします。0 なら除外しません。"},
			{key: tachibanasource.KeyTachibanaNightlyExcludeSymbols, label: "除外: 銘柄コード", hint: "対象外にする銘柄です。カンマまたは空白区切りで最大 500 件です。", placeholder: "例: 1321, 1570"},
		},
	},
	tachibanaScreenGroup(),
	{
		id:          "tachibana-intraday",
		name:        "立花 日中の EVENT 接続・REST 時価補助",
		description: "日中は確定した監視リストで EVENT I/F に常時接続し、REST の時価取得は少数・間隔を空けた補助だけに使います。",
		note:        sourceNote,
		fields: []opsField{
			{key: tachibanasource.KeyTachibanaEventMaxConnects, label: "EVENT 接続・切断の 1 日の予算（回）", hint: "1〜20 の整数です。既定は 10 回で、使い切ると購読の入替を止めて警告します。"},
			{key: tachibanasource.KeyTachibanaRestQuoteMinInterval, label: "REST 時価補助: 最短間隔（秒）", hint: "10〜3600 の整数です。既定は 60 秒（60 秒に 1 回以下）です。"},
			{key: tachibanasource.KeyTachibanaRestQuoteRequestsPerRound, label: "REST 時価補助: 1 回あたりの要求数", hint: "1〜3 の整数です。既定は 1 です。"},
		},
	},
}

func numText(f float64) string { return fmt.Sprintf("%g", f) }

func tachibanaScreenGroup() opsGroup {
	group := opsGroup{
		id:          "tachibana-screen",
		name:        "立花 スクリーニング（指標・重み・上位件数）",
		description: "前日の日足から翌日の監視銘柄を選ぶ daily_screen の指標です。kabu の /ranking の種別に寄せています。",
		note:        sourceNote + screenSourceNote,
	}
	for _, ind := range tachibanasource.TachibanaScreenIndicators {
		label := screenIndicatorLabels[ind]
		group.fields = append(group.fields,
			opsField{key: tachibanasource.TachibanaScreenWeightKey(ind), label: screenWeightLabel + label, hint: "0〜100 の数値です。"},
			opsField{key: tachibanasource.TachibanaScreenTopNKey(ind), label: screenTopNLabel + label, hint: "0〜120 の整数です。0 ならこの指標は使いません。"},
		)
	}
	return group
}
