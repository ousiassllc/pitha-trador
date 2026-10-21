package bootstrap

import (
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperrisk"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/repoportfolio"
)

// riskRepositories are the tables the Risk Engine reads and writes.
type riskRepositories struct {
	killSwitch *system.KillSwitchRepository
	settings   *system.RuntimeSettingsRepository
	snapshots  *market.SnapshotRepository
	positions  *trading.PositionRepository
	orders     *trading.OrderRepository
}

// riskSignals are the live health/failure signals the Risk Engine's
// FR-RISK-2 detectors poll: the market-data and Jev API HealthCheckers
// (also FR-RISK-7's auto-resume recovery checks) and the consecutive
// Broker API/DB write failure streaks.
type riskSignals struct {
	marketData risk.HealthChecker
	jevAPI     risk.HealthChecker
	brokerAPI  risk.FailureCounter
	dbWrite    risk.FailureCounter
}

// newRiskEngine builds the Paper Trading Risk Engine (this build only
// runs Paper Trading, so config/risk.yaml's paper limits apply). The Paper
// operating capital and exposure limits are overlaid with the Settings
// screen's runtime_settings on every evaluation (papersettings, issue #804),
// so a saved value applies without a restart; risk.yaml stays the fallback.
// A Live Engine must be built WITHOUT the overlay and the capital source
// below: Live keeps using config/risk.yaml only.
func newRiskEngine(limits config.RiskLimits, repos riskRepositories, signals riskSignals, closer risk.PositionCloser, notifier risk.Notifier) *risk.Engine {
	return risk.NewEngine(risk.Config{
		Limits:            limits,
		LimitsOverlay:     papersettings.Overlay(repos.settings),
		StockBucket:       paperrisk.StockBucket(repos.positions, repos.settings),
		KillSwitch:        repos.killSwitch,
		Settings:          repos.settings,
		Snapshots:         repos.snapshots,
		Portfolio:         repoportfolio.New(repos.positions, limits.InitialCapital, repoportfolio.WithCapitalSource(paperrisk.CapitalSource(repos.settings, limits))),
		Positions:         repos.positions,
		Orders:            repos.orders,
		MarketDataHealth:  signals.marketData,
		JevAPIHealth:      signals.jevAPI,
		BrokerAPIFailures: signals.brokerAPI,
		DBWriteFailures:   signals.dbWrite,
		Closer:            closer,
		Notifier:          notifier,
		Calendar:          marketcalendar.TSE,
	})
}
