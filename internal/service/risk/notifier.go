package risk

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Notifier is notified of every Kill Switch state transition and
// threshold-warning event non-functional.md §5.2 requires an immediate
// Slack alert for that this package can itself observe.
// internal/service/notify.SlackNotifier is the real implementation (a
// later composition-root step wires it in - same deferred-wiring
// precedent as PortfolioProvider/PositionCloser/HealthChecker above);
// NoopNotifier is the placeholder default.
//
// Five of §5.2's eight alert categories key off KillSwitchTriggered's
// ev.Reason: market_data_down (市場データ停止), broker_api_error
// (kabuステーションAPI異常), unexpected_position (想定外ポジション検知),
// operator_heartbeat_timeout (操作者ハートビートタイムアウト), and every
// reason together for "Kill Switch発動（自動再開可否・発動理由を含む）" -
// autoResumable reports FR-RISK-7's classification so the message can
// also state whether this reason resolves itself or needs a manual
// resume. KillSwitchAutoResumed covers the remaining "自動再開" half of
// §5.2's combined "Kill Switch自動再開・手動再開待ち" bullet.
// DailyLossWarning is this package's own remaining §5.2 item ("日次損失
// 上限接近（例: 上限の80%到達）", warning.go). "Jev APIエラー率上昇" is
// internal/service/jev.AlertNotifier's own responsibility - a different
// package's own call outcomes drive it, not anything Risk Engine
// observes.
type Notifier interface {
	KillSwitchTriggered(ctx context.Context, ev domain.KillSwitchEvent, autoResumable bool) error
	KillSwitchAutoResumed(ctx context.Context, ev domain.KillSwitchEvent) error
	DailyLossWarning(ctx context.Context, currentPct, limitPct float64) error
}

// NoopNotifier is the placeholder Notifier used until a later
// composition-root step wires internal/service/notify.SlackNotifier (and,
// for the Wails desktop shell, cmd/desktop's own App) in.
type NoopNotifier struct{}

func (NoopNotifier) KillSwitchTriggered(context.Context, domain.KillSwitchEvent, bool) error {
	return nil
}

func (NoopNotifier) KillSwitchAutoResumed(context.Context, domain.KillSwitchEvent) error { return nil }

func (NoopNotifier) DailyLossWarning(context.Context, float64, float64) error { return nil }
