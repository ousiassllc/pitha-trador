package alerts

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
)

// BrokerNotices returns the session.Notifier for the 立花 adapter's operator
// notices (login overdue, login contention, unread 書面, announced API release
// / 書面 update date; issue #727, non-functional.md §5.2): every notice goes
// to the Activity feed (broker_notice event) and, when Slack is configured,
// to Slack. The adapter itself logs each notice at WARN. activity may be nil.
// Notices never carry credentials or session URLs.
func (c Channels) BrokerNotices(activity *activityfeed.Service) session.Notifier {
	return session.NotifierFunc(func(ctx context.Context, n session.Notice) {
		if activity != nil {
			activity.ObserveBrokerNotice(n.Message)
		}
		if c.Slack == nil {
			return
		}
		if err := c.Slack.PostMessage(ctx, "[立花証券 e支店API] "+n.Message); err != nil {
			slog.Warn("alerts: broker notice could not be posted to Slack", "kind", string(n.Kind), "error", err)
		}
	})
}
