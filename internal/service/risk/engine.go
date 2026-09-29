package risk

import (
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/sizing"
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
	ReasonMaxTotalExposurePct     = sizing.ReasonMaxTotalExposurePct
	ReasonMaxPositionPerSymbolPct = sizing.ReasonMaxPositionPerSymbolPct
	ReasonMaxDailyLossPct         = "max_daily_loss_pct"
	ReasonMaxConsecutiveLosses    = "max_consecutive_losses"
	ReasonMaxSpreadBps            = "max_spread_bps"
	ReasonMaxTradeLossPct         = sizing.ReasonMaxTradeLossPct
	// ReasonRiskEngineError is the fail-closed rejection when Check cannot
	// read the state a limit needs (DB error, missing snapshot/spread).
	ReasonRiskEngineError = "risk_engine_error"
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

	// Snapshots supplies the latest price/spread for FR-RISK-1's
	// max_spread_bps and max_trade_loss_pct (position sizing) checks. A
	// nil Snapshots skips those two checks (no snapshot data to check
	// against); a non-nil one rejects an instrument with no usable
	// snapshot (fail closed).
	Snapshots *repository.SnapshotRepository

	// StopLossPct is the FR-EXIT-2 initial stop distance (percent)
	// position sizing assumes; defaults to sizing.DefaultStopLossPct.
	StopLossPct float64

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

	// Calendar gates the operator-heartbeat timeout and the market-data/
	// Jev API health detectors to 東証立会時間 (session.go). nil leaves
	// them running around the clock.
	Calendar MarketCalendar

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
	stopLossPct      float64
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
	calendar          MarketCalendar
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
	if cfg.StopLossPct <= 0 {
		cfg.StopLossPct = sizing.DefaultStopLossPct
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = DefaultFailureThreshold
	}
	return &Engine{
		limits:            cfg.Limits,
		killSwitch:        cfg.KillSwitch,
		settings:          cfg.Settings,
		snapshots:         cfg.Snapshots,
		stopLossPct:       cfg.StopLossPct,
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
		calendar:          cfg.Calendar,
	}
}

// Limits returns the Paper/Live threshold set this Engine enforces.
func (e *Engine) Limits() config.RiskLimits { return e.limits }

func activeReasons(events []domain.KillSwitchEvent) string {
	reasons := make([]string, len(events))
	for i, ev := range events {
		reasons[i] = ev.Reason
	}
	return fmt.Sprint(reasons)
}
