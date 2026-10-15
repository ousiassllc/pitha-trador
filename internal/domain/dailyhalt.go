package domain

import "time"

// Daily entry-halt reasons (docs/requirements/functional.md §4.7
// FR-RISK-8): the three day-trading rules that stop new entries for the
// rest of the trading day while still allowing exits. Unlike the Kill Switch
// they never force-close positions and are not rows of kill_switch_events.
const (
	DailyHaltReasonLossLimit    = "daily_loss_limit_jpy"
	DailyHaltReasonProfitLock   = "profit_lock"
	DailyHaltReasonMaxTrades    = "max_daily_trades"
	dailyHaltLabelLossLimit     = "1日の損失上限"
	dailyHaltLabelProfitLock    = "利益確保（profit lock）"
	dailyHaltLabelMaxTrades     = "1日の最大取引回数"
	dailyHaltLabelUnknownReason = "新規エントリー停止"
)

// DailyEntryHalt is one trading day's entry halt: which rule fired, a
// human-readable account of the figures (Message, Japanese, for the screen,
// the notification and the log) and when.
type DailyEntryHalt struct {
	// Day is the trading day (JST, "2006-01-02") the halt applies to; it
	// lapses on the next one.
	Day      string
	Reason   string
	Message  string
	HaltedAt time.Time
}

// Label is the operator-facing name of h's rule.
func (h DailyEntryHalt) Label() string {
	switch h.Reason {
	case DailyHaltReasonLossLimit:
		return dailyHaltLabelLossLimit
	case DailyHaltReasonProfitLock:
		return dailyHaltLabelProfitLock
	case DailyHaltReasonMaxTrades:
		return dailyHaltLabelMaxTrades
	}
	return dailyHaltLabelUnknownReason
}
