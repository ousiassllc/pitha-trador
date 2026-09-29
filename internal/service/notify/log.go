package notify

import (
	"context"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// LogNotifier writes every non-functional.md §5.2 alert to a structured
// slog.Logger at Warn level. It implements the same risk.Notifier,
// jev.AlertNotifier and selfimprove.Notifier methods SlackNotifier does,
// and is the one channel every entrypoint always has: cmd/server runs
// headlessly (no Wails native toast) and SLACK_WEBHOOK_URL is optional, so
// without it an alert could otherwise reach no channel at all.
type LogNotifier struct {
	logger *slog.Logger
}

// NewLogNotifier returns a LogNotifier writing to logger, or to
// slog.Default() when logger is nil.
func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogNotifier{logger: logger}
}

func (n *LogNotifier) KillSwitchTriggered(ctx context.Context, ev domain.KillSwitchEvent, autoResumable bool) error {
	n.logger.WarnContext(ctx, "alert: kill switch triggered",
		"reason", ev.Reason, "reason_label", reasonLabel(ev.Reason), "auto_resumable", autoResumable)
	return nil
}

func (n *LogNotifier) KillSwitchAutoResumed(ctx context.Context, ev domain.KillSwitchEvent) error {
	n.logger.WarnContext(ctx, "alert: kill switch auto-resumed", "reason", ev.Reason, "reason_label", reasonLabel(ev.Reason))
	return nil
}

func (n *LogNotifier) DailyLossWarning(ctx context.Context, currentPct, limitPct float64) error {
	n.logger.WarnContext(ctx, "alert: daily loss approaching limit", "current_pct", currentPct, "limit_pct", limitPct)
	return nil
}

func (n *LogNotifier) JevAPIErrorRateExceeded(ctx context.Context, rate, threshold float64) error {
	n.logger.WarnContext(ctx, "alert: jev api error rate exceeded", "rate", rate, "threshold", threshold)
	return nil
}

func (n *LogNotifier) ProposalApplied(ctx context.Context, proposal domain.PolicyProposal) error {
	n.logger.WarnContext(ctx, "alert: policy proposal applied", "proposal_id", proposal.ID)
	return nil
}

func (n *LogNotifier) ProposalRolledBack(ctx context.Context, proposal domain.PolicyProposal, reason string) error {
	n.logger.WarnContext(ctx, "alert: policy proposal rolled back", "proposal_id", proposal.ID, "reason", reason)
	return nil
}

func (n *LogNotifier) AIStageSkipped(ctx context.Context, stage string, cause error) error {
	n.logger.WarnContext(ctx, "alert: self-improvement AI stage skipped, retry next business day", "stage", stage, "error", cause)
	return nil
}
