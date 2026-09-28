package selfimprove

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Notifier is notified of every apply/rollback the Governor performs
// (overview.md §8 "GOV->>SLACK: 適用を通知" / "GOV->>SLACK: ロールバックを
// 通知", FR-SELFIMPROVE-6's "Slack通知する"). internal/service/notify.
// SlackNotifier (or LogNotifier when no Slack webhook is configured)
// implements it; internal/bootstrap injects it via WithNotifier.
// NoopNotifier, the default, lets Governor be constructed and tested
// without one.
type Notifier interface {
	ProposalApplied(ctx context.Context, proposal domain.PolicyProposal) error
	ProposalRolledBack(ctx context.Context, proposal domain.PolicyProposal, reason string) error
}

// NoopNotifier is Notifier's do-nothing default.
type NoopNotifier struct{}

// ProposalApplied does nothing.
func (NoopNotifier) ProposalApplied(context.Context, domain.PolicyProposal) error { return nil }

// ProposalRolledBack does nothing.
func (NoopNotifier) ProposalRolledBack(context.Context, domain.PolicyProposal, string) error {
	return nil
}
