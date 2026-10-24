package dailyguardsettings

import "math"

// Warning is a display-only notice about the effective configuration. It
// never changes what the risk engine does: nothing is rejected, nothing is
// disabled (issue #782, functional.md FR-RISK-8). Code is a stable slug (the
// Settings screen's test id suffix), Message the Japanese text.
type Warning struct {
	Code    string
	Message string
}

// Warning codes.
const (
	WarnCapitalMismatch          = "capital-mismatch"
	WarnLossLimitAboveKillSwitch = "loss-limit-above-kill-switch"
	WarnLiveLossLimit            = "live-loss-limit"
	WarnLiveProfitLock           = "live-profit-lock"
	WarnLiveMaxTrades            = "live-max-trades"
)

// RiskReference is the part of config/risk.yaml's limits (the active mode's)
// the consistency check compares the daily guard against: initial_capital
// and max_daily_loss_pct, the denominator and the rate of the Kill Switch's
// daily loss limit. A value <= 0 is "unset" and skips the checks that need it.
type RiskReference struct {
	InitialCapitalJPY float64
	MaxDailyLossPct   float64
}

// CheckConsistency compares the resolved daily guard with risk.yaml:
//
//   - the operating capital differs from initial_capital (the rate-based
//     limits and position sizing are measured against initial_capital);
//   - the yen loss limit is larger than the Kill Switch amount
//     (initial_capital × max_daily_loss_pct), so the forced liquidation
//     fires before the entry halt does.
//
// The Kill Switch amount is rounded to a whole yen the same way resolveRule
// rounds a percentage-derived limit (divide by 100 first, then math.Round), so
// the two are compared on the same footing: equal figures never warn because
// of floating-point noise (700,000 × 0.7%), and a half-yen amount rounds up
// on both sides (500,050 at 1% is 5,001 yen for the limit and the Kill Switch
// alike). Beyond that there is no tolerance: any whole-yen difference warns,
// equality does not. An unset
// operating capital with a percentage stored is reported by the 計算結果
// panel itself and is not repeated here.
func CheckConsistency(r Resolved, ref RiskReference) []Warning {
	var warnings []Warning
	if r.CapitalJPY > 0 && ref.InitialCapitalJPY > 0 && r.CapitalJPY != ref.InitialCapitalJPY {
		warnings = append(warnings, Warning{WarnCapitalMismatch,
			"運用資金 " + FormatYen(r.CapitalJPY) + " 円が、リスク設定の運用資金 " + FormatYen(ref.InitialCapitalJPY) +
				" 円と異なります。率ベースの上限とポジションサイズは、リスク設定の運用資金を基準に計算されます"})
	}
	if ref.InitialCapitalJPY > 0 && ref.MaxDailyLossPct > 0 {
		killSwitchJPY := math.Round(ref.InitialCapitalJPY / 100 * ref.MaxDailyLossPct)
		if r.Limits.LossLimitEnabled() && r.Limits.LossLimitJPY > killSwitchJPY {
			warnings = append(warnings, Warning{WarnLossLimitAboveKillSwitch,
				"円の損失上限 " + FormatYen(r.Limits.LossLimitJPY) + " 円が率ベース上限（Kill Switch）" + FormatYen(killSwitchJPY) +
					" 円（運用資金 × 日次損失率）より大きく、新規停止より先に強制決済が発動します"})
		}
	}
	return warnings
}

// LiveReadiness lists every rule that is disabled, for the pre-Live check on
// the Settings screen. It only warns: Paper runs with any rule off, and
// whether a disabled rule should block Live (and what enables the rules by
// default) is not decided yet.
func LiveReadiness(r Resolved) []Warning {
	var warnings []Warning
	l := r.Limits
	if !l.LossLimitEnabled() {
		msg := "1日の損失上限が無効です"
		if r.LossLimit.NeedsCapital() {
			msg += "（運用資金が未設定）"
		}
		warnings = append(warnings, Warning{WarnLiveLossLimit, msg})
	}
	if !l.ProfitLockEnabled() {
		msg := "利益確保が無効です"
		switch {
		case l.ProfitLockGivebackPct > 0:
			msg += "（発動額が無効）"
		case l.ProfitLockTriggerJPY > 0:
			msg += "（許容戻し率が 0）"
		default:
			msg += "（発動額と許容戻し率の両方が必要）"
		}
		warnings = append(warnings, Warning{WarnLiveProfitLock, msg})
	}
	if !l.MaxTradesEnabled() {
		warnings = append(warnings, Warning{WarnLiveMaxTrades, "最大取引回数が無効です"})
	}
	return warnings
}
