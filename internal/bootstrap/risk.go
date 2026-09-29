package bootstrap

import (
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/notify"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/repoportfolio"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// alertChannels is every non-functional.md §5.2 alert destination this
// process has. log is always present (cmd/server has no native toast,
// and SLACK_WEBHOOK_URL is optional); slack is nil when
// secrets.SlackWebhookURL is empty.
type alertChannels struct {
	log   *notify.LogNotifier
	slack *notify.SlackNotifier
}

func newAlertChannels(secrets config.Secrets) alertChannels {
	channels := alertChannels{log: notify.NewLogNotifier(nil)}
	if secrets.SlackWebhookURL != "" {
		channels.slack = notify.NewSlackNotifier(notify.Config{WebhookURL: secrets.SlackWebhookURL})
	}
	return channels
}

// riskNotifier fans the Risk Engine's Kill Switch/daily-loss alerts out to
// every channel: the structured log, Slack (when configured), and each
// entrypoint-specific extra Notifier (cmd/desktop's App - native OS toast
// plus in-window status events, which only a Wails process can provide).
func (c alertChannels) riskNotifier(extra []risk.Notifier) risk.Notifier {
	notifiers := []risk.Notifier{c.log}
	if c.slack != nil {
		notifiers = append(notifiers, c.slack)
	}
	notifiers = append(notifiers, extra...)
	return risk.NewMultiNotifier(notifiers...)
}

// jevAlerts returns the jev.AlertNotifier for Jev API error-rate alerts:
// Slack when configured (non-functional.md §5.2 lists this alert as
// Slack-bound, not a native toast), otherwise the structured log.
func (c alertChannels) jevAlerts() jev.AlertNotifier {
	if c.slack != nil {
		return c.slack
	}
	return c.log
}

// selfImproveNotifier returns the selfimprove.Notifier for policy
// proposal apply/rollback alerts: Slack when configured, otherwise the
// structured log (same precedence as jevAlerts).
func (c alertChannels) selfImproveNotifier() selfimprove.Notifier {
	if c.slack != nil {
		return c.slack
	}
	return c.log
}

// riskRepositories are the tables the Risk Engine reads and writes.
type riskRepositories struct {
	killSwitch *repository.KillSwitchRepository
	settings   *repository.RuntimeSettingsRepository
	snapshots  *repository.SnapshotRepository
	positions  *repository.PositionRepository
	orders     *repository.OrderRepository
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
// runs Paper Trading, so config/risk.yaml's paper limits apply).
func newRiskEngine(limits config.RiskLimits, repos riskRepositories, signals riskSignals, closer risk.PositionCloser, notifier risk.Notifier) *risk.Engine {
	return risk.NewEngine(risk.Config{
		Limits:            limits,
		KillSwitch:        repos.killSwitch,
		Settings:          repos.settings,
		Snapshots:         repos.snapshots,
		Portfolio:         repoportfolio.New(repos.positions, limits.InitialCapital),
		Positions:         repos.positions,
		Orders:            repos.orders,
		MarketDataHealth:  signals.marketData,
		JevAPIHealth:      signals.jevAPI,
		BrokerAPIFailures: signals.brokerAPI,
		DBWriteFailures:   signals.dbWrite,
		Closer:            closer,
		Notifier:          notifier,
	})
}
