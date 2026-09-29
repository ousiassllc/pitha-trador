package notify

import (
	"context"
	"fmt"
)

// MaintenanceFailed implements internal/service/scheduler/maintenance.
// Notifier: a daily housekeeping task (DBバックアップ等) has failed several
// times in a row and needs the operator's attention.
func (n *SlackNotifier) MaintenanceFailed(ctx context.Context, task string, consecutiveFailures int, cause error) error {
	text := fmt.Sprintf(
		":rotating_light: 定期メンテナンス「%s」が%d回連続で失敗しています。退避先の接続状態・空き容量を確認してください: %v",
		task, consecutiveFailures, cause,
	)
	return n.PostMessage(ctx, text)
}

// MaintenanceFailed implements internal/service/scheduler/maintenance.
// Notifier by logging the failure streak.
func (n *LogNotifier) MaintenanceFailed(ctx context.Context, task string, consecutiveFailures int, cause error) error {
	n.logger.WarnContext(ctx, "alert: maintenance task failing repeatedly",
		"task", task, "consecutive_failures", consecutiveFailures, "error", cause)
	return nil
}

// MaintenanceNotifier is the method internal/service/scheduler/
// maintenance.Notifier declares for itself (same layering precedent as
// risk.Notifier); both channels implement it.
type MaintenanceNotifier interface {
	MaintenanceFailed(ctx context.Context, task string, consecutiveFailures int, cause error) error
}

// MaintenanceChannel returns Slack when configured (non-nil), otherwise
// the structured log, for maintenance-failure alerts.
func MaintenanceChannel(logNotifier *LogNotifier, slack *SlackNotifier) MaintenanceNotifier {
	if slack != nil {
		return slack
	}
	return logNotifier
}
