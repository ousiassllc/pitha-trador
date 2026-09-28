package risk

import (
	"context"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// MultiNotifier fans out every Notifier call to each of Notifiers in
// order, for a deployment that needs more than one notification channel
// at once (issue #48: cmd/desktop's App, a native OS toast Notifier,
// alongside internal/service/notify.SlackNotifier, an Incoming Webhook
// Notifier - non-functional.md §5.2's alerts go to both).
//
// One channel failing (e.g. the Slack webhook unreachable) must not
// suppress the other (e.g. the native OS toast still firing), so every
// Notifier in Notifiers is always called regardless of an earlier one's
// error; every non-nil error is collected via errors.Join so a caller
// that does check the return value still sees all of them.
type MultiNotifier struct {
	Notifiers []Notifier
}

// NewMultiNotifier returns a MultiNotifier fanning out to notifiers.
func NewMultiNotifier(notifiers ...Notifier) MultiNotifier {
	return MultiNotifier{Notifiers: notifiers}
}

func (m MultiNotifier) KillSwitchTriggered(ctx context.Context, ev domain.KillSwitchEvent, autoResumable bool) error {
	var errs []error
	for _, n := range m.Notifiers {
		if err := n.KillSwitchTriggered(ctx, ev, autoResumable); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m MultiNotifier) KillSwitchAutoResumed(ctx context.Context, ev domain.KillSwitchEvent) error {
	var errs []error
	for _, n := range m.Notifiers {
		if err := n.KillSwitchAutoResumed(ctx, ev); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m MultiNotifier) DailyLossWarning(ctx context.Context, currentPct, limitPct float64) error {
	var errs []error
	for _, n := range m.Notifiers {
		if err := n.DailyLossWarning(ctx, currentPct, limitPct); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
