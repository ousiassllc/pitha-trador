package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

const defaultHTTPTimeout = 10 * time.Second

// reasonLabels renders every domain.KillReason* value as the Japanese
// phrase non-functional.md §5.2's alert list uses, so a Slack message
// reads naturally instead of showing the raw snake_case reason string.
var reasonLabels = map[string]string{
	domain.KillReasonDailyLossLimit:           "日次損失上限到達",
	domain.KillReasonConsecutiveLosses:        "連敗上限到達",
	domain.KillReasonMarketDataDown:           "市場データ停止",
	domain.KillReasonJevAPIDown:               "Jev API連続失敗",
	domain.KillReasonBrokerAPIError:           "kabuステーションAPI異常",
	domain.KillReasonUnexpectedPosition:       "想定外ポジション検知",
	domain.KillReasonFillDiscrepancy:          "約定差異検知",
	domain.KillReasonDBWriteFailure:           "DB書き込み失敗継続",
	domain.KillReasonOperatorHeartbeatTimeout: "操作者ハートビートタイムアウト",
}

func reasonLabel(reason string) string {
	if label, ok := reasonLabels[reason]; ok {
		return label
	}
	return reason
}

// Config configures a SlackNotifier.
type Config struct {
	// WebhookURL is the Slack Incoming Webhook URL (.env.example's
	// SLACK_WEBHOOK_URL, architecture/overview.md §2).
	WebhookURL string
	// HTTPClient defaults to &http.Client{Timeout: 10 * time.Second}.
	HTTPClient *http.Client
}

// SlackNotifier posts non-functional.md §5.2's eight immediate-alert
// categories to a Slack Incoming Webhook. It implements
// internal/service/risk.Notifier's three methods and
// internal/service/jev.AlertNotifier's one method directly (structural
// typing - see this package's doc.go).
type SlackNotifier struct {
	webhookURL string
	httpClient *http.Client
}

// NewSlackNotifier returns a SlackNotifier posting to cfg.WebhookURL.
func NewSlackNotifier(cfg Config) *SlackNotifier {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &SlackNotifier{webhookURL: cfg.WebhookURL, httpClient: httpClient}
}

type webhookPayload struct {
	Text string `json:"text"`
}

// PostMessage posts text as a Slack Incoming Webhook message
// ({"text": text}, Slack's documented Incoming Webhook payload shape). A
// non-2xx response is returned as an error carrying the response body.
func (n *SlackNotifier) PostMessage(ctx context.Context, text string) error {
	body, err := json.Marshal(webhookPayload{Text: text})
	if err != nil {
		return fmt.Errorf("notify: encode slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: build slack webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notify: post slack webhook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notify: slack webhook returned status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// KillSwitchTriggered implements internal/service/risk.Notifier: it
// covers five of §5.2's eight alert categories - 市場データ停止
// (market_data_down), kabuステーションAPI異常 (broker_api_error), Kill
// Switch発動（自動再開可否・発動理由を含む）(every reason),
// 操作者ハートビートタイムアウト (operator_heartbeat_timeout), and
// 想定外ポジション検知 (unexpected_position) - keyed by ev.Reason, plus
// "手動再開待ち" (functional.md FR-RISK-7's manual-resume-only half of
// §5.2's combined "自動再開・手動再開待ち" bullet) when !autoResumable.
func (n *SlackNotifier) KillSwitchTriggered(ctx context.Context, ev domain.KillSwitchEvent, autoResumable bool) error {
	resume := "手動再開が必要です"
	if autoResumable {
		resume = "発動条件の解消後に自動再開されます"
	}
	text := fmt.Sprintf(
		":rotating_light: Kill Switch発動: %s（reason=%s）\n発動時刻: %s\n%s\n詳細: %s",
		reasonLabel(ev.Reason), ev.Reason, ev.TriggeredAt.Format(time.RFC3339), resume, ev.DetailJSON,
	)
	return n.PostMessage(ctx, text)
}

// KillSwitchAutoResumed implements internal/service/risk.Notifier's
// remaining half of §5.2's "Kill Switch自動再開・手動再開待ち" bullet
// (FR-RISK-7's auto-resume half).
func (n *SlackNotifier) KillSwitchAutoResumed(ctx context.Context, ev domain.KillSwitchEvent) error {
	text := fmt.Sprintf(
		":white_check_mark: Kill Switch自動再開: %s（reason=%s）\n発動時刻: %s",
		reasonLabel(ev.Reason), ev.Reason, ev.TriggeredAt.Format(time.RFC3339),
	)
	return n.PostMessage(ctx, text)
}

// DailyLossWarning implements internal/service/risk.Notifier's
// remaining §5.2 alert category: "日次損失上限接近（例: 上限の80%到達）".
func (n *SlackNotifier) DailyLossWarning(ctx context.Context, currentPct, limitPct float64) error {
	text := fmt.Sprintf(
		":warning: 日次損失上限接近: 現在の日次損失率 %.2f%%（上限 %.2f%% の %.0f%%到達）",
		currentPct*100, limitPct*100, currentPct/limitPct*100,
	)
	return n.PostMessage(ctx, text)
}

// JevAPIErrorRateExceeded implements internal/service/jev.AlertNotifier:
// §5.2's "Jev APIエラー率上昇（しきい値超過）".
func (n *SlackNotifier) JevAPIErrorRateExceeded(ctx context.Context, rate, threshold float64) error {
	text := fmt.Sprintf(
		":warning: Jev APIエラー率上昇: 直近のエラー率 %.2f%%（しきい値 %.2f%% を超過）",
		rate*100, threshold*100,
	)
	return n.PostMessage(ctx, text)
}

// ProposalApplied implements internal/service/selfimprove.Notifier:
// overview.md §8 "GOV->>SLACK: 適用を通知" (FR-SELFIMPROVE-5).
func (n *SlackNotifier) ProposalApplied(ctx context.Context, proposal domain.PolicyProposal) error {
	version := ""
	if proposal.AppliedPolicyVersion != nil {
		version = *proposal.AppliedPolicyVersion
	}
	text := fmt.Sprintf(
		":white_check_mark: 自己改善ループ: Solの提案(id=%d)がpolicy_version=%sとして適用されました",
		proposal.ID, version,
	)
	return n.PostMessage(ctx, text)
}

// ProposalRolledBack implements internal/service/selfimprove.Notifier:
// overview.md §8 "GOV->>SLACK: ロールバックを通知" (FR-SELFIMPROVE-6).
func (n *SlackNotifier) ProposalRolledBack(ctx context.Context, proposal domain.PolicyProposal, reason string) error {
	text := fmt.Sprintf(
		":rotating_light: 自己改善ループ: 提案(id=%d)の適用を自動ロールバックしました: %s",
		proposal.ID, reason,
	)
	return n.PostMessage(ctx, text)
}
