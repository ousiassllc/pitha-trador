package settings

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// riskReferenceSource is the optional capability of an OperationalSettings
// that knows the Paper limits in force (*opsettings.Service, via
// WithRiskReference). Without it - or with ok false - the 整合チェック is
// skipped. It is reference data only: nothing here changes the risk engine.
type riskReferenceSource interface {
	RiskReference(ctx context.Context) (ref dailyguardsettings.RiskReference, ok bool, err error)
}

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
	items := dailyGuardDerivedItems(resolved)
	if ref, ok := h.riskReference(ctx); ok {
		items = append(items, consistencyItems(dailyguardsettings.CheckConsistency(resolved, ref))...)
	}
	return append(items, liveReadinessItems(dailyguardsettings.LiveReadiness(resolved))...)
}

// riskReference reads what the 整合チェック compares the daily guard with; ok
// is false when it is unavailable (not wired, or a read failure, which is
// logged), which skips the check.
func (h *SettingsHandler) riskReference(ctx context.Context) (ref dailyguardsettings.RiskReference, ok bool) {
	src, wired := h.ops.(riskReferenceSource)
	if !wired {
		return dailyguardsettings.RiskReference{}, false
	}
	ref, ok, err := src.RiskReference(ctx)
	if err != nil {
		slog.Error("settings: read Paper risk limits for the consistency check; skipping it", "error", err)
		return dailyguardsettings.RiskReference{}, false
	}
	return ref, ok
}

// consistencyItems is the 整合チェック block: risk.yaml's initial_capital and
// Kill Switch rate against the daily guard (display only).
func consistencyItems(warnings []dailyguardsettings.Warning) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{{Key: "consistency", Heading: true, Label: "整合チェック", Detail: "（risk.yaml との比較・表示のみ）"}}
	if len(warnings) == 0 {
		return append(items, molecules.SettingDerivedItem{Key: "consistency-ok", Label: "問題なし"})
	}
	return append(items, warningItems(warnings)...)
}

// liveReadinessItems is the Live 前チェック block. It warns about each
// disabled rule and never rejects anything: the Live default-enable criteria
// are still awaiting the owner's decision (issue #782).
func liveReadinessItems(warnings []dailyguardsettings.Warning) []molecules.SettingDerivedItem {
	items := []molecules.SettingDerivedItem{{Key: "live", Heading: true, Label: "Live 前チェック", Detail: "警告のみ（拒否はしない）。Live の既定有効化の基準は決定待ち"}}
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

// dailyGuardDerivedItems describes each rule's effective value and where it
// comes from (運用資金 × %, a direct yen value, or disabled).
func dailyGuardDerivedItems(r dailyguardsettings.Resolved) []molecules.SettingDerivedItem {
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
		item.Detail = "（円で直接指定・互換設定）"
	case rule.NeedsCapital():
		item.Detail = "（運用資金が未設定のため無効）"
	}
	return item
}
