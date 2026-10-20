package query

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Attention codes of HealthToday.AttentionCodes (issue #808): fixed, never
// free text. The two health alerts keep the codes of the in-app banner
// (issue #802), so the banner and this tool name the same condition alike.
const (
	// AttentionNoPaperTradesToday: a trading day, the morning session is over
	// and no Paper position was opened today.
	AttentionNoPaperTradesToday = "no_paper_trades_today"
	// AttentionNoActiveModel: no meta-labeling model is adopted.
	AttentionNoActiveModel = "no_active_model"
	// AttentionKabuNotLoggedIn: the #802 alert of the same name is active.
	AttentionKabuNotLoggedIn = AlertKabuNotLoggedIn
	// AttentionMarketDataIdle: the #802 alert of the same name is active.
	AttentionMarketDataIdle = AlertMarketDataIdle
	// AttentionMissingSession: a recent trading session has no snapshot.
	AttentionMissingSession = "missing_session"
	// AttentionDailyGuardHalted: today's daily entry halt is in force.
	AttentionDailyGuardHalted = "daily_guard_halted"
	// AttentionKillSwitchActive: the Kill Switch is active.
	AttentionKillSwitchActive = "kill_switch_active"
	// AttentionRetrainSkipped: the newest nightly retraining was skipped for
	// lack of data and no model was trained since.
	AttentionRetrainSkipped = "retrain_skipped"
)

// AttentionCodes returns every code HealthToday may report, in the order it
// reports them.
func AttentionCodes() []string {
	return []string{
		AttentionNoPaperTradesToday, AttentionNoActiveModel, AttentionKabuNotLoggedIn, AttentionMarketDataIdle,
		AttentionMissingSession, AttentionDailyGuardHalted, AttentionKillSwitchActive, AttentionRetrainSkipped,
	}
}

// Values of HealthMarketData.KabuLogin and .MarketData. "not_watched": outside
// the hours the detector watches (non-trading day, or outside the kabu window
// 08:45-15:30 / the sessions), where silence is no fault. "unknown" (kabu
// only): inside the window but the app's detector is not running, so its
// state cannot be read.
const (
	KabuLoginOK          = "ok"
	KabuLoginNotLoggedIn = "not_logged_in"
	KabuLoginUnknown     = "unknown"
	MarketDataRunning    = "running"
	MarketDataIdleStatus = "idle"
	StatusNotWatched     = "not_watched"
)

const (
	// missingLookbackDays is how far back a missing session is looked for.
	missingLookbackDays = 7
	// noTradesAfterMinute is when no_paper_trades_today may first be raised,
	// in minutes after JST midnight: the morning session's close (11:30), so
	// the quiet start of the day is no alarm.
	noTradesAfterMinute = 11*60 + 30
	// retrainSkipFreshHours is how long a skipped retraining counts as news.
	retrainSkipFreshHours = 24
)

// HealthPaperTrades counts the Paper positions of today (JST).
type HealthPaperTrades struct {
	OpenedCount int `json:"opened_count"`
	ClosedCount int `json:"closed_count"`
}

// HealthMetaModel is the adopted meta-labeling model, if any.
type HealthMetaModel struct {
	Adopted bool    `json:"adopted"`
	Version *string `json:"version"`
	// TrainedAt is when the adopted model was trained (UTC RFC3339).
	TrainedAt *string `json:"trained_at"`
}

// HealthBootstrap is the Paper-only bootstrap entry (issue #798).
type HealthBootstrap struct {
	// Enabled is the Settings switch paper.bootstrap_entry_enabled (default off).
	Enabled bool `json:"enabled"`
	// Effective is whether the legacy entry rule is in force: the switch is on
	// and no model is adopted. This MCP serves the Paper database only; the
	// app never applies the bootstrap in Live whatever the switch says.
	Effective bool `json:"effective"`
	// SignalsToday is how many of today's signals were decided on it.
	SignalsToday int `json:"signals_today"`
}

// HealthMarketData is the market-data side of the #802 health alerts.
type HealthMarketData struct {
	KabuLogin  string `json:"kabu_login"`
	MarketData string `json:"market_data"`
	// AppRunning is whether the app's detector has checked recently.
	AppRunning bool `json:"app_running"`
	// Alerts are the #802 alerts active now: the same value as
	// collection_status.alerts.
	Alerts []Alert `json:"alerts"`
}

// HealthDailyGuard is today's daily entry halt (FR-RISK-8).
type HealthDailyGuard struct {
	Halted bool `json:"halted"`
	// Reason is the halt's fixed code (daily_loss_limit_jpy, profit_lock,
	// max_daily_trades); the message text is never returned.
	Reason   *string `json:"reason"`
	HaltedAt *string `json:"halted_at"`
}

// HealthKillSwitch is the Kill Switch state.
type HealthKillSwitch struct {
	Active          bool `json:"active"`
	UnresolvedCount int  `json:"unresolved_count"`
	// Since is when the newest unresolved event was triggered (UTC RFC3339).
	Since *string `json:"since"`
	// Reasons are the fixed reason codes of the unresolved events.
	Reasons []string `json:"reasons"`
	// FlagSet is the system.killed flag, which keeps the system killed even
	// when no unresolved event remains.
	FlagSet bool `json:"flag_set"`
}

// HealthRetrain is the newest skipped nightly retraining (issue #799).
type HealthRetrain struct {
	// Skipped is whether it is within the last 24 hours and no model has been
	// trained since.
	Skipped         bool    `json:"skipped"`
	LatestSkippedAt *string `json:"latest_skipped_at"`
	ReasonCode      *string `json:"reason_code"`
}

// Health is health_today's data (issue #808).
type Health struct {
	// Date is today in JST; TradingDay whether it is a TSE trading day.
	Date       string `json:"date"`
	TradingDay bool   `json:"trading_day"`
	// NeedsAttention is true exactly when AttentionCodes is not empty.
	NeedsAttention bool     `json:"needs_attention"`
	AttentionCodes []string `json:"attention_codes"`

	PaperTrades HealthPaperTrades `json:"paper_trades"`
	MetaModel   HealthMetaModel   `json:"meta_model"`
	Bootstrap   HealthBootstrap   `json:"bootstrap"`
	MarketData  HealthMarketData  `json:"market_data"`
	// MissingSessionDates are the trading days of the last 7 days (today
	// included once its session closed) that have no market snapshot although
	// an earlier snapshot exists: a real gap, which raises missing_session.
	MissingSessionDates []string `json:"missing_session_dates"`
	// InsufficientHistoryDates are the trading days of the same span with no
	// snapshot and none before them either: the history does not reach back
	// that far (first start), so they cannot be judged. Never a warning.
	InsufficientHistoryDates []string         `json:"insufficient_history_dates"`
	DailyGuard               HealthDailyGuard `json:"daily_guard"`
	KillSwitch               HealthKillSwitch `json:"kill_switch"`
	Retrain                  HealthRetrain    `json:"retrain"`
}

// HealthToday gathers today's health in one read. Every part reuses the
// state the other tools and the in-app banner read; nothing here is raw
// market data or a secret (runtime_settings is read for a fixed key list
// only, see settingsReader).
func HealthToday(ctx context.Context, q Q, now time.Time) (Health, error) {
	jstNow := now.In(JST)
	dayStart := time.Date(jstNow.Year(), jstNow.Month(), jstNow.Day(), 0, 0, 0, 0, JST)
	today := Range{From: dayStart, To: dayStart.AddDate(0, 0, 1)}
	h := Health{
		Date:       dayStart.Format(time.DateOnly),
		TradingDay: marketcalendar.TSE.IsTradingDay(now),
	}

	var err error
	if h.PaperTrades, err = healthPaperTrades(ctx, q, today); err != nil {
		return Health{}, err
	}
	if h.MetaModel, err = healthMetaModel(ctx, q); err != nil {
		return Health{}, err
	}
	if h.Bootstrap, err = healthBootstrap(ctx, q, today, h.MetaModel.Adopted); err != nil {
		return Health{}, err
	}
	if h.MarketData, err = healthMarketData(ctx, q, now); err != nil {
		return Health{}, err
	}
	if h.MissingSessionDates, h.InsufficientHistoryDates, err = sessionGaps(ctx, q, missingRange(now, dayStart)); err != nil {
		return Health{}, err
	}
	if h.DailyGuard, err = healthDailyGuard(ctx, q, h.Date); err != nil {
		return Health{}, err
	}
	if h.KillSwitch, err = healthKillSwitch(ctx, q); err != nil {
		return Health{}, err
	}
	if h.Retrain, err = healthRetrain(ctx, q, now); err != nil {
		return Health{}, err
	}
	h.AttentionCodes = attentionCodes(h, jstNow)
	h.NeedsAttention = len(h.AttentionCodes) > 0
	return h, nil
}

// missingRange is the days checked for a missing session: the seven days
// before today, and today too once its close has passed.
func missingRange(now, dayStart time.Time) Range {
	to := dayStart
	if closeAt, ok := marketcalendar.TSE.CloseAt(now); ok && !now.Before(closeAt) {
		to = dayStart.AddDate(0, 0, 1)
	}
	return Range{From: dayStart.AddDate(0, 0, -missingLookbackDays), To: to}
}

// attentionCodes derives the codes from the gathered state. Conditions that
// are about the clock (no trades yet, a login or market-data outage, a gap in
// the recent sessions) are raised on trading days only, so a weekend or
// holiday never alarms; state-like ones (no model, a kill switch, a halt, a
// skipped retraining) are real whatever the day.
func attentionCodes(h Health, jstNow time.Time) []string {
	codes := []string{}
	if h.TradingDay && jstNow.Hour()*60+jstNow.Minute() >= noTradesAfterMinute && h.PaperTrades.OpenedCount == 0 {
		codes = append(codes, AttentionNoPaperTradesToday)
	}
	if !h.MetaModel.Adopted {
		codes = append(codes, AttentionNoActiveModel)
	}
	for _, a := range h.MarketData.Alerts {
		// HealthAlerts already withholds an alert outside its window.
		codes = append(codes, a.Code)
	}
	if len(h.MissingSessionDates) > 0 {
		codes = append(codes, AttentionMissingSession)
	}
	if h.DailyGuard.Halted {
		codes = append(codes, AttentionDailyGuardHalted)
	}
	if h.KillSwitch.Active {
		codes = append(codes, AttentionKillSwitchActive)
	}
	if h.Retrain.Skipped {
		codes = append(codes, AttentionRetrainSkipped)
	}
	return orderedCodes(codes)
}

// orderedCodes sorts codes into AttentionCodes order (a health alert is added
// in the order the detector lists them, oldest first).
func orderedCodes(codes []string) []string {
	out := []string{}
	for _, c := range AttentionCodes() {
		for _, got := range codes {
			if got == c {
				out = append(out, c)
				break
			}
		}
	}
	return out
}
