// Package alerts is the composition root's set of non-functional.md §5.2
// alert destinations (structured log, optional Slack) and the per-consumer
// notifier each service receives. It lives under internal/bootstrap
// because it only adapts config.Secrets to the services' own Notifier
// interfaces.
package alerts

import (
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/notify"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
	"github.com/ousiassllc/pitha-trador/internal/service/risk/multinotify"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// Channels is every non-functional.md §5.2 alert destination this
// process has. log is always present (cmd/server has no native toast,
// and SLACK_WEBHOOK_URL is optional); slack is nil when
// secrets.SlackWebhookURL is empty.
type Channels struct {
	Log   *notify.LogNotifier
	Slack *notify.SlackNotifier
}

// New builds the channels secrets configures.
func New(secrets config.Secrets) Channels {
	channels := Channels{Log: notify.NewLogNotifier(nil)}
	if secrets.SlackWebhookURL != "" {
		channels.Slack = notify.NewSlackNotifier(notify.Config{WebhookURL: secrets.SlackWebhookURL})
	}
	return channels
}

// RiskNotifier fans the Risk Engine's Kill Switch/daily-loss alerts out to
// every channel: the structured log, Slack (when configured), and each
// entrypoint-specific extra Notifier (cmd/desktop's App - native OS toast
// plus in-window status events, which only a Wails process can provide).
func (c Channels) RiskNotifier(extra []risk.Notifier) risk.Notifier {
	notifiers := []risk.Notifier{c.Log}
	if c.Slack != nil {
		notifiers = append(notifiers, c.Slack)
	}
	notifiers = append(notifiers, extra...)
	return multinotify.New(notifiers...)
}

// JevAlerts returns the jev.AlertNotifier for Jev API error-rate alerts:
// Slack when configured (non-functional.md §5.2 lists this alert as
// Slack-bound, not a native toast), otherwise the structured log.
func (c Channels) JevAlerts() jev.AlertNotifier {
	if c.Slack != nil {
		return c.Slack
	}
	return c.Log
}

// SelfImproveNotifier returns the selfimprove.Notifier for policy
// proposal apply/rollback alerts: Slack when configured, otherwise the
// structured log (same precedence as JevAlerts).
func (c Channels) SelfImproveNotifier() selfimprove.Notifier {
	if c.Slack != nil {
		return c.Slack
	}
	return c.Log
}
