// Package derivedpanel builds the rows of a Settings group's 計算結果 panel
// (molecules.SettingDerivedItem) from already-read values. It is pure: reading
// the settings and logging a failed read stay in the handler
// (internal/web/handler/settings), which keeps that package under the
// directory line limit.
package derivedpanel

import (
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// ConsistencyItems is the 整合チェック block: risk.yaml's initial_capital and
// Kill Switch rate against the daily guard (display only).
func ConsistencyItems(warnings []dailyguardsettings.Warning) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{{Key: "consistency", Heading: true, Label: "整合チェック", Help: []string{"config/risk.yaml の運用資金・日次損失率との比較です。表示のみで、取引の動きは変わりません。"}}}
	if len(warnings) == 0 {
		return append(items, molecules.SettingDerivedItem{Key: "consistency-ok", Label: "問題なし"})
	}
	return append(items, warningItems(warnings)...)
}

// LiveReadinessItems is the Live 前チェック block. It warns about each
// disabled rule and never rejects anything: the Live default-enable criteria
// are not decided yet.
func LiveReadinessItems(warnings []dailyguardsettings.Warning) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{{Key: "live", Heading: true, Label: "Live 前チェック", Help: []string{"Live で使う前の確認です。無効なルールを警告するだけで、保存は拒否しません。"}}}
	if len(warnings) == 0 {
		return append(items, molecules.SettingDerivedItem{Key: "live-ok", Label: "3つのルールすべて有効"})
	}
	return append(items, warningItems(warnings)...)
}

func warningItems(warnings []dailyguardsettings.Warning) []molecules.SettingDerivedItem {
	items := make([]molecules.SettingDerivedItem, len(warnings))
	for i, w := range warnings {
		items[i] = molecules.SettingDerivedItem{Key: w.Code, Label: w.Message, Warn: true}
	}
	return items
}

// DailyGuardItems describes each rule's effective value and where it comes
// from (運用資金 × %, a direct yen value, or disabled).
func DailyGuardItems(r dailyguardsettings.Resolved) []molecules.SettingDerivedItem {
	capital := molecules.SettingDerivedItem{Key: "capital", Label: "運用資金", Value: dailyguardsettings.FormatYen(r.CapitalJPY) + " 円"}
	if r.CapitalJPY <= 0 {
		capital.Value, capital.Off = "未設定", true
	}
	lossLimit := ruleItem("loss-limit", "1日の損失上限", r.CapitalJPY, r.LossLimit)
	trigger := ruleItem("profit-lock-trigger", "利益確保の発動額", r.CapitalJPY, r.ProfitLockTrigger)

	limits := r.Limits
	lock := molecules.SettingDerivedItem{Key: "profit-lock", Label: "利益確保"}
	switch {
	case limits.ProfitLockEnabled():
		lock.Value = "最高損益から " + dailyguardsettings.FormatNumber(limits.ProfitLockGivebackPct) + "% 戻したら停止"
	case limits.ProfitLockGivebackPct > 0:
		lock.Value, lock.Detail, lock.Off = "無効", "（発動額が無効のため）", true
	case limits.ProfitLockTriggerJPY > 0:
		lock.Value, lock.Detail, lock.Off = "無効", "（許容戻し率が 0 のため）", true
	default:
		lock.Value, lock.Off = "無効", true
	}

	trades := molecules.SettingDerivedItem{Key: "max-trades", Label: "最大取引回数", Value: strconv.Itoa(limits.MaxTrades) + " 回"}
	if !limits.MaxTradesEnabled() {
		trades.Value, trades.Off = "無効", true
	}
	return []molecules.SettingDerivedItem{capital, lossLimit, trigger, lock, trades}
}

func ruleItem(key, label string, capital float64, rule dailyguardsettings.Rule) molecules.SettingDerivedItem {
	item := molecules.SettingDerivedItem{Key: key, Label: label, Value: "無効", Off: true}
	switch {
	case rule.Source == dailyguardsettings.SourcePercent && rule.JPY > 0:
		item.Value, item.Off = dailyguardsettings.FormatYen(rule.JPY)+" 円", false
		item.Detail = "（運用資金 " + dailyguardsettings.FormatYen(capital) + " 円 × " + dailyguardsettings.FormatNumber(rule.Pct) + "%）"
	case rule.Source == dailyguardsettings.SourcePercent:
		item.Detail = "（運用資金 × " + dailyguardsettings.FormatNumber(rule.Pct) + "% が 1 円未満のため）"
	case rule.Source == dailyguardsettings.SourceDirect:
		item.Value, item.Off = dailyguardsettings.FormatYen(rule.JPY)+" 円", false
		item.Detail = "（円で直接指定）"
	case rule.NeedsCapital():
		item.Detail = "（運用資金が未設定のため無効）"
	}
	return item
}
