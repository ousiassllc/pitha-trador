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
//
// Notifications are best-effort, like the Risk Engine's: Governor calls
// ProposalApplied/ProposalRolledBack only after the apply/rollback is
// already committed to runtime_settings and policy_proposals, so a
// returned error is logged and otherwise ignored - it never turns the
// completed apply/rollback into a failure (DailyResult.Applied/
// RetriedApplied/RolledBack still report it) and is not retried.
type Notifier interface {
	ProposalApplied(ctx context.Context, proposal domain.PolicyProposal) error
	ProposalRolledBack(ctx context.Context, proposal domain.PolicyProposal, reason string) error
	// AIStageSkipped reports that the external Sol/Opus API failed and
	// the named stage ("sol"/"opus") was skipped for today; the daily
	// batch retries it the next business day (overview.md §8).
	AIStageSkipped(ctx context.Context, stage string, cause error) error
}

// NoopNotifier is Notifier's do-nothing default.
type NoopNotifier struct{}

// ProposalApplied does nothing.
func (NoopNotifier) ProposalApplied(context.Context, domain.PolicyProposal) error { return nil }

// ProposalRolledBack does nothing.
func (NoopNotifier) ProposalRolledBack(context.Context, domain.PolicyProposal, string) error {
	return nil
}

// AIStageSkipped does nothing.
func (NoopNotifier) AIStageSkipped(context.Context, string, error) error { return nil }
