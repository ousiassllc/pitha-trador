package settings

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// dailyGuardDerived reads the effective daily entry-halt configuration for
// the group's 計算結果 panel. A read failure is logged and the panel says so
// (the rows themselves still render) rather than failing the Settings screen
// or showing values that may be stale.
func (h *SettingsHandler) dailyGuardDerived(ctx context.Context) []molecules.SettingDerivedItem {
	resolved, err := h.ops.DailyGuard(ctx)
	if err != nil {
		slog.Error("settings: read daily guard configuration; showing the derived panel as unreadable", "error", err)
		return []molecules.SettingDerivedItem{{Key: "error", Label: "計算結果を読み取れませんでした", Off: true}}
	}
	return dailyGuardDerivedItems(resolved)
}

// dailyGuardDerivedItems describes each rule's effective value and where it
// comes from (運用資金 × %, a direct yen value, or disabled).
func dailyGuardDerivedItems(r dailyguardsettings.Resolved) []molecules.SettingDerivedItem {
	capital := molecules.SettingDerivedItem{Key: "capital", Label: "運用資金", Value: formatYen(r.CapitalJPY) + " 円"}
	if r.CapitalJPY <= 0 {
		capital.Value, capital.Off = "未設定", true
	}
	lossLimit := ruleItem("loss-limit", "1日の損失上限", r.CapitalJPY, r.LossLimit)
	trigger := ruleItem("profit-lock-trigger", "利益確保の発動額", r.CapitalJPY, r.ProfitLockTrigger)

	limits := r.Limits
	lock := molecules.SettingDerivedItem{Key: "profit-lock", Label: "利益確保"}
	switch {
	case limits.ProfitLockEnabled():
		lock.Value = "最高損益から " + formatNumber(limits.ProfitLockGivebackPct) + "% 戻したら停止"
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
		item.Value, item.Off = formatYen(rule.JPY)+" 円", false
		item.Detail = "（運用資金 " + formatYen(capital) + " 円 × " + formatNumber(rule.Pct) + "%）"
	case rule.Source == dailyguardsettings.SourcePercent:
		item.Detail = "（運用資金 × " + formatNumber(rule.Pct) + "% が 1 円未満のため）"
	case rule.Source == dailyguardsettings.SourceDirect:
		item.Value, item.Off = formatYen(rule.JPY)+" 円", false
		item.Detail = "（円で直接指定・互換設定）"
	case rule.NeedsCapital():
		item.Detail = "（運用資金が未設定のため無効）"
	}
	return item
}

// formatNumber renders v without a trailing ".0" (1, 0.5, 33.33).
func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// formatYen renders v with thousands separators (5,000; 1,234,567.5). It is
// not shared with organisms.yen, which rounds to whole yen and adds a ¥ sign.
func formatYen(v float64) string {
	text := formatNumber(v)
	intPart, frac, hasFrac := strings.Cut(text, ".")
	negative := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	out := b.String()
	if negative {
		out = "-" + out
	}
	if hasFrac {
		out += "." + frac
	}
	return out
}
