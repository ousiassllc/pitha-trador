package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// The daily entry-halt rules (issue #753, FR-RISK-8): a day-trading
// discipline layer on top of the FR-RISK-1/2 limits. Once the day's
// realized + unrealized P&L has fallen to the loss limit, has given back
// too much of its peak (profit lock), or the day's trade count is used up,
// Check rejects every new entry for the rest of the trading day. Exits are
// untouched (Check only gates entries), and - unlike daily_loss_limit - no
// Kill Switch is raised and no position is force-closed.
//
// The thresholds are runtime_settings rows edited on the Settings screen
// (dailyguardsettings); 0 / unset disables a rule. The halt is latched in
// the system.daily_guard row together with the day's P&L peak the profit
// lock trails, so a later recovery does not reopen the day and a restart
// does not forget it. The row is keyed by the JST trading day: the first
// evaluation of the next day starts a fresh state, which is the automatic
// reset on the next business day (non-trading days see no entries at all).
// The halt is evaluated on every Check and by RunPeriodicChecks
// (CheckDailyGuard), so a rule reached without any candidate still halts,
// notifies and logs once.

const settingKeyDailyGuard = "system.daily_guard"

// tradingDayZone bounds the trading day: TSE trades in JST.
var tradingDayZone = time.FixedZone("JST", 9*60*60)

const tradingDayLayout = "2006-01-02"

// DailyActivity is the day's trading result the daily entry-halt rules
// read from the PortfolioProvider.
type DailyActivity struct {
	// PnLJPY is today's (JST) realized P&L of positions closed today plus
	// the unrealized P&L of every open position, in yen (negative = loss).
	PnLJPY float64
	// Trades is how many positions were opened today (still open or closed).
	Trades int
}

// dailyGuardState is the system.daily_guard row.
type dailyGuardState struct {
	Day string `json:"day"`
	// PeakPnLJPY is the day's highest P&L seen so far (never below 0), the
	// reference the profit lock's give-back is measured from.
	PeakPnLJPY  float64 `json:"peak_pnl_jpy"`
	HaltReason  string  `json:"halt_reason,omitempty"`
	HaltMessage string  `json:"halt_message,omitempty"`
	HaltedAt    string  `json:"halted_at,omitempty"`
}

func (s dailyGuardState) halt() (domain.DailyEntryHalt, bool) {
	if s.HaltReason == "" {
		return domain.DailyEntryHalt{}, false
	}
	at, _ := time.Parse(time.RFC3339Nano, s.HaltedAt)
	return domain.DailyEntryHalt{Day: s.Day, Reason: s.HaltReason, Message: s.HaltMessage, HaltedAt: at}, true
}

func (e *Engine) loadDailyGuard(ctx context.Context, day string) (dailyGuardState, error) {
	raw, ok, err := e.settings.Get(ctx, settingKeyDailyGuard)
	if err != nil {
		return dailyGuardState{}, fmt.Errorf("risk: get %s: %w", settingKeyDailyGuard, err)
	}
	var state dailyGuardState
	if ok {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return dailyGuardState{}, fmt.Errorf("risk: decode setting %s=%q: %w", settingKeyDailyGuard, raw, err)
		}
	}
	if state.Day != day {
		// A new trading day (or the first ever): nothing carries over.
		return dailyGuardState{Day: day}, nil
	}
	return state, nil
}

func (e *Engine) saveDailyGuard(ctx context.Context, state dailyGuardState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("risk: encode setting %s: %w", settingKeyDailyGuard, err)
	}
	if err := e.settings.Set(ctx, settingKeyDailyGuard, string(data), e.now()); err != nil {
		return fmt.Errorf("risk: set %s: %w", settingKeyDailyGuard, err)
	}
	return nil
}

// reasonEnabled reports whether the rule behind a halt reason still applies.
func reasonEnabled(l dailyguardsettings.Limits, reason string) bool {
	switch reason {
	case domain.DailyHaltReasonLossLimit:
		return l.LossLimitEnabled()
	case domain.DailyHaltReasonProfitLock:
		return l.ProfitLockEnabled()
	case domain.DailyHaltReasonMaxTrades:
		return l.MaxTradesEnabled()
	}
	return false
}

// dailyGuardBreach returns the first rule activity (with the day's peak)
// has reached, in the order loss limit, profit lock, trade count, and its
// operator-facing message; "" when none has.
func dailyGuardBreach(l dailyguardsettings.Limits, a DailyActivity, peak float64) (reason, message string) {
	switch {
	case l.LossLimitEnabled() && a.PnLJPY <= -l.LossLimitJPY:
		return domain.DailyHaltReasonLossLimit,
			fmt.Sprintf("当日損益 %+.0f円 が損失上限 -%.0f円 に達しました。", a.PnLJPY, l.LossLimitJPY)
	case l.ProfitLockEnabled() && peak >= l.ProfitLockTriggerJPY && a.PnLJPY <= peak-peak*l.ProfitLockGivebackPct/100:
		return domain.DailyHaltReasonProfitLock,
			fmt.Sprintf("当日損益 %+.0f円 が最高 %+.0f円 から %.0f%% 以上戻したため、利益を確保します（発動額 %.0f円）。",
				a.PnLJPY, peak, l.ProfitLockGivebackPct, l.ProfitLockTriggerJPY)
	case l.MaxTradesEnabled() && a.Trades >= l.MaxTrades:
		return domain.DailyHaltReasonMaxTrades,
			fmt.Sprintf("当日の取引回数 %d回 が上限 %d回 に達しました。", a.Trades, l.MaxTrades)
	}
	return "", ""
}

// dailyGuard evaluates the daily entry-halt rules and returns today's halt,
// or nil while entries are allowed. A halt reached by this call is notified
// and logged once. With no rule enabled it does nothing (no reads of the
// day's positions).
func (e *Engine) dailyGuard(ctx context.Context) (*domain.DailyEntryHalt, error) {
	halt, fresh, err := e.evaluateDailyGuard(ctx)
	if err != nil || halt == nil {
		return nil, err
	}
	if fresh {
		slog.Warn("risk: daily entry halt: new entries stopped for the rest of the trading day",
			"reason", halt.Reason, "message", halt.Message, "day", halt.Day)
		if err := e.notifier.DailyEntryHalted(ctx, *halt); err != nil {
			slog.Error("risk: daily entry halt notification failed", "reason", halt.Reason, "error", err)
		}
	}
	return halt, nil
}

func (e *Engine) evaluateDailyGuard(ctx context.Context) (halt *domain.DailyEntryHalt, fresh bool, err error) {
	limits, err := dailyguardsettings.Load(ctx, e.settings)
	if err != nil {
		return nil, false, fmt.Errorf("risk: read daily guard thresholds: %w", err)
	}
	if !limits.AnyEnabled() {
		return nil, false, nil
	}

	e.dailyGuardMu.Lock()
	defer e.dailyGuardMu.Unlock()
	day := e.now().In(tradingDayZone).Format(tradingDayLayout)
	state, err := e.loadDailyGuard(ctx, day)
	if err != nil {
		return nil, false, err
	}
	changed := false
	if h, ok := state.halt(); ok {
		if reasonEnabled(limits, h.Reason) {
			return &h, false, nil
		}
		// The operator disabled the rule that halted today: lift the halt.
		state.HaltReason, state.HaltMessage, state.HaltedAt = "", "", ""
		changed = true
	}

	activity, err := e.portfolio.DailyActivity(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("risk: read daily activity: %w", err)
	}
	if activity.PnLJPY > state.PeakPnLJPY {
		state.PeakPnLJPY = activity.PnLJPY
		changed = true
	}
	if reason, message := dailyGuardBreach(limits, activity, state.PeakPnLJPY); reason != "" {
		state.HaltReason, state.HaltMessage, state.HaltedAt = reason, message, e.now().UTC().Format(time.RFC3339Nano)
		changed, fresh = true, true
	}
	if changed {
		if err := e.saveDailyGuard(ctx, state); err != nil {
			return nil, false, err
		}
	}
	h, ok := state.halt()
	if !ok {
		return nil, false, nil
	}
	return &h, fresh, nil
}

// CheckDailyGuard runs the daily entry-halt evaluation without a candidate
// (RunPeriodicChecks calls it every minute), so a halt is raised, notified
// and shown even when no entry is being attempted, and the profit lock's
// peak keeps tracking the day.
func (e *Engine) CheckDailyGuard(ctx context.Context) error {
	_, err := e.dailyGuard(ctx)
	return err
}

// DailyHalt returns today's daily entry halt, or nil while none applies. It
// only reads the latched state (the 1-minute CheckDailyGuard and every Check
// do the evaluating), so the screen polling it never writes.
func (e *Engine) DailyHalt(ctx context.Context) (*domain.DailyEntryHalt, error) {
	limits, err := dailyguardsettings.Load(ctx, e.settings)
	if err != nil {
		return nil, fmt.Errorf("risk: read daily guard thresholds: %w", err)
	}
	state, err := e.loadDailyGuard(ctx, e.now().In(tradingDayZone).Format(tradingDayLayout))
	if err != nil {
		return nil, err
	}
	h, ok := state.halt()
	if !ok || !reasonEnabled(limits, h.Reason) {
		return nil, nil
	}
	return &h, nil
}
