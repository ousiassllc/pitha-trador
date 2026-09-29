package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// FR-RISK-1 rejection reasons: internal/service/policy.RiskChecker's
// Check implementation below returns one of these (optionally with
// "key=value" detail appended) as its reason string. Policy Engine
// prefixes it with "risk_engine_rejected: " before persisting it to
// trade_signals.reject_reason (policy/engine.go's
// ReasonRiskEngineRejected).
const (
	ReasonSystemPaused            = "system_paused"
	ReasonKillSwitchActive        = "kill_switch_active"
	ReasonCooldownAfterLoss       = "cooldown_after_loss"
	ReasonMaxOpenPositions        = "max_open_positions"
	ReasonMaxTotalExposurePct     = "max_total_exposure_pct"
	ReasonMaxPositionPerSymbolPct = "max_position_per_symbol_pct"
	ReasonMaxDailyLossPct         = "max_daily_loss_pct"
	ReasonMaxConsecutiveLosses    = "max_consecutive_losses"
	ReasonMaxSpreadBps            = "max_spread_bps"
)

// runtime_settings keys this package owns (er.md §runtime_settings).
// SettingKeyLastUIHeartbeatAt is exported so internal/web/middleware can
// write it without importing internal/repository directly
// (docs/architecture/overview.md §3 layer rule).
const (
	SettingKeyLastUIHeartbeatAt          = "system.last_ui_heartbeat_at"
	settingKeySystemPaused               = "system.paused"
	settingKeySystemKilled               = "system.killed"
	settingKeyDailyLossWarningNotifiedAt = "system.daily_loss_warning_notified_at"
)

// autoResumableReasons is FR-RISK-7's automatic-resume set. Every other
// valid domain.KillReason* is manual-resume-only.
var autoResumableReasons = map[string]bool{
	domain.KillReasonMarketDataDown:           true,
	domain.KillReasonJevAPIDown:               true,
	domain.KillReasonOperatorHeartbeatTimeout: true,
}

// Config configures a new Engine (NewEngine). KillSwitch and Settings are
// required; every other field has a documented placeholder default so an
// Engine can be constructed - and FR-RISK-1's Kill-Switch/pause gate and
// dead-man's switch already work correctly - before the sub-scopes that
// wire real Portfolio/Closer/HealthChecker implementations in exist.
type Config struct {
	// Limits is the Paper- or Live-specific threshold set this Engine
	// enforces (config.RiskConfig.Paper or .Live, functional.md §4.7's
	// table). A zero HeartbeatTimeoutMinutes disables FR-RISK-6 (Paper
	// mode: the dead-man's switch is Live-only).
	Limits config.RiskLimits

	KillSwitch *repository.KillSwitchRepository
	Settings   *repository.RuntimeSettingsRepository

	// Snapshots supplies the latest spread for FR-RISK-1's
	// max_spread_bps check. A nil Snapshots skips that one check (no
	// snapshot data to check against).
	Snapshots *repository.SnapshotRepository

	// Portfolio defaults to ZeroPortfolioProvider{} (portfolio.go).
	Portfolio PortfolioProvider
	// Closer defaults to NoopPositionCloser{} (closer.go).
	Closer PositionCloser
	// MarketDataHealth/JevAPIHealth default to AlwaysHealthy{} (health.go).
	MarketDataHealth HealthChecker
	JevAPIHealth     HealthChecker

	// BrokerAPIFailures/DBWriteFailures are the consecutive-failure
	// streaks of the Broker (kabuステーション) API and of DB writes that
	// drive the broker_api_error/db_write_failure Kill Switches once they
	// reach FailureThreshold (monitor.go). A nil counter disables that one
	// check.
	BrokerAPIFailures FailureCounter
	DBWriteFailures   FailureCounter
	// FailureThreshold is the streak length at which those two counters
	// trigger FR-RISK-2's "一定回数継続". Defaults to
	// DefaultFailureThreshold.
	FailureThreshold int

	// Positions and Orders enable CheckPositionReconciliation
	// (unexpected_position/fill_discrepancy). Either being nil disables it.
	Positions *repository.PositionRepository
	Orders    *repository.OrderRepository

	// Notifier defaults to NoopNotifier{} (notifier.go).
	Notifier Notifier

	// Now defaults to time.Now. Tests override it for deterministic
	// cooldown/heartbeat-timeout checks.
	Now func() time.Time
}

// Engine is the Risk Engine (functional.md §4.7): Policy Engine's final
// approve/reject gate (FR-RISK-1, Check) and the Running/Paused/Killed
// system state machine (FR-RISK-2〜7, state.go/autoresume.go).
type Engine struct {
	limits           config.RiskLimits
	killSwitch       *repository.KillSwitchRepository
	settings         *repository.RuntimeSettingsRepository
	snapshots        *repository.SnapshotRepository
	portfolio        PortfolioProvider
	closer           PositionCloser
	marketDataHealth HealthChecker
	jevAPIHealth     HealthChecker

	brokerAPIFailures FailureCounter
	dbWriteFailures   FailureCounter
	failureThreshold  int
	positions         *repository.PositionRepository
	orders            *repository.OrderRepository
	notifier          Notifier
	now               func() time.Time
}

// NewEngine returns an Engine built from cfg, applying every documented
// placeholder default for a zero-valued optional field. It panics if
// cfg.KillSwitch or cfg.Settings is nil - both are required for every
// FR-RISK-2〜7 operation.
func NewEngine(cfg Config) *Engine {
	if cfg.KillSwitch == nil {
		panic("risk: NewEngine: cfg.KillSwitch is required")
	}
	if cfg.Settings == nil {
		panic("risk: NewEngine: cfg.Settings is required")
	}
	if cfg.Portfolio == nil {
		cfg.Portfolio = ZeroPortfolioProvider{}
	}
	if cfg.Closer == nil {
		cfg.Closer = NoopPositionCloser{}
	}
	if cfg.MarketDataHealth == nil {
		cfg.MarketDataHealth = AlwaysHealthy{}
	}
	if cfg.JevAPIHealth == nil {
		cfg.JevAPIHealth = AlwaysHealthy{}
	}
	if cfg.Notifier == nil {
		cfg.Notifier = NoopNotifier{}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = DefaultFailureThreshold
	}
	return &Engine{
		limits:            cfg.Limits,
		killSwitch:        cfg.KillSwitch,
		settings:          cfg.Settings,
		snapshots:         cfg.Snapshots,
		portfolio:         cfg.Portfolio,
		closer:            cfg.Closer,
		marketDataHealth:  cfg.MarketDataHealth,
		jevAPIHealth:      cfg.JevAPIHealth,
		brokerAPIFailures: cfg.BrokerAPIFailures,
		dbWriteFailures:   cfg.DBWriteFailures,
		failureThreshold:  cfg.FailureThreshold,
		positions:         cfg.Positions,
		orders:            cfg.Orders,
		notifier:          cfg.Notifier,
		now:               cfg.Now,
	}
}

// Limits returns the Paper/Live threshold set this Engine enforces, for
// callers (e.g. a later Execution sub-scope's position sizing) that need
// max_trade_loss_pct: Check does not enforce it itself, since no order
// size/stop-loss distance is available at Check's (instrumentID,
// direction) call site (FR-RISK-1's table lists it, but as a
// sizing input rather than a pre-trade reject condition).
func (e *Engine) Limits() config.RiskLimits { return e.limits }

// Check implements internal/service/policy.RiskChecker: Risk Engine's
// final say over every Policy Engine trade candidate (FR-RISK-1).
//
// Rejection reasons are: ReasonKillSwitchActive/ReasonSystemPaused (Kill
// Switch/manual-pause active, FR-RISK-2/FR-RISK-4), ReasonCooldownAfterLoss
// (risk.yaml cooldown_after_loss_minutes has not elapsed since the most
// recent loss - a lightweight, time-based gate that is not itself logged
// to kill_switch_events, unlike consecutive_losses reaching
// max_consecutive_losses below), then each FR-RISK-1 limit in the table's
// order except max_trade_loss_pct (see Limits's doc comment).
//
// Breaching max_daily_loss_pct or max_consecutive_losses also raises a
// Kill Switch (FR-RISK-2), latching the rejection in place for every
// subsequent candidate (via the Kill Switch state check above) rather
// than only this one.
func (e *Engine) Check(ctx context.Context, instrumentID int64, direction string) (bool, string) {
	state, events, err := e.State(ctx)
	if err != nil {
		return false, fmt.Sprintf("risk_engine_error: %v", err)
	}
	switch state {
	case domain.SystemStateKilled:
		return false, fmt.Sprintf("%s: %s", ReasonKillSwitchActive, activeReasons(events))
	case domain.SystemStatePaused:
		return false, ReasonSystemPaused
	}

	now := e.now()
	if lastLoss, err := e.portfolio.LastLossAt(ctx); err == nil && !lastLoss.IsZero() {
		cooldown := time.Duration(e.limits.CooldownAfterLossMinutes) * time.Minute
		if resumeAt := lastLoss.Add(cooldown); now.Before(resumeAt) {
			return false, fmt.Sprintf("%s: retry_after=%s", ReasonCooldownAfterLoss, resumeAt.Format(time.RFC3339))
		}
	}

	if count, err := e.portfolio.OpenPositionCount(ctx); err == nil && count >= e.limits.MaxOpenPositions {
		return false, fmt.Sprintf("%s: count=%d max=%d", ReasonMaxOpenPositions, count, e.limits.MaxOpenPositions)
	}
	if pct, err := e.portfolio.TotalExposurePct(ctx); err == nil && pct >= e.limits.MaxTotalExposurePct {
		return false, fmt.Sprintf("%s: exposure_pct=%.4f max=%.4f", ReasonMaxTotalExposurePct, pct, e.limits.MaxTotalExposurePct)
	}
	if pct, err := e.portfolio.SymbolExposurePct(ctx, instrumentID); err == nil && pct >= e.limits.MaxPositionPerSymbolPct {
		return false, fmt.Sprintf("%s: exposure_pct=%.4f max=%.4f", ReasonMaxPositionPerSymbolPct, pct, e.limits.MaxPositionPerSymbolPct)
	}

	if pct, err := e.portfolio.DailyLossPct(ctx); err == nil && pct >= e.limits.MaxDailyLossPct {
		_, _ = e.triggerIfNotActive(ctx, domain.KillReasonDailyLossLimit, map[string]any{
			"daily_loss_pct": pct, "max_daily_loss_pct": e.limits.MaxDailyLossPct,
		})
		return false, fmt.Sprintf("%s: daily_loss_pct=%.4f max=%.4f", ReasonMaxDailyLossPct, pct, e.limits.MaxDailyLossPct)
	}
	if losses, err := e.portfolio.ConsecutiveLosses(ctx); err == nil && losses >= e.limits.MaxConsecutiveLosses {
		_, _ = e.triggerIfNotActive(ctx, domain.KillReasonConsecutiveLosses, map[string]any{
			"consecutive_losses": losses, "max_consecutive_losses": e.limits.MaxConsecutiveLosses,
		})
		return false, fmt.Sprintf("%s: consecutive_losses=%d max=%d", ReasonMaxConsecutiveLosses, losses, e.limits.MaxConsecutiveLosses)
	}

	if spreadBps, ok := e.latestSpreadBps(ctx, instrumentID); ok && spreadBps > e.limits.MaxSpreadBps {
		return false, fmt.Sprintf("%s: spread_bps=%.2f max=%.2f", ReasonMaxSpreadBps, spreadBps, e.limits.MaxSpreadBps)
	}

	return true, ""
}

func (e *Engine) latestSpreadBps(ctx context.Context, instrumentID int64) (float64, bool) {
	if e.snapshots == nil {
		return 0, false
	}
	snapshots, err := e.snapshots.ListByInstrument(ctx, instrumentID, 1)
	if err != nil || len(snapshots) == 0 || snapshots[0].SpreadBps == nil {
		return 0, false
	}
	return *snapshots[0].SpreadBps, true
}

func activeReasons(events []domain.KillSwitchEvent) string {
	reasons := make([]string, len(events))
	for i, ev := range events {
		reasons[i] = ev.Reason
	}
	return fmt.Sprint(reasons)
}
