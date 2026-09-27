package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/notify"
)

// captureWebhook returns an httptest.Server recording every posted
// {"text": ...} payload's text into *got, plus a notify.SlackNotifier
// configured to post to it.
func captureWebhook(t *testing.T, got *[]string) (*notify.SlackNotifier, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode webhook payload: %v", err)
		}
		*got = append(*got, payload.Text)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	n := notify.NewSlackNotifier(notify.Config{WebhookURL: srv.URL})
	return n, srv.Close
}

func TestSlackNotifier_PostMessage_SendsTextPayload(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	if err := n.PostMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("got = %v, want [\"hello\"]", got)
	}
}

func TestSlackNotifier_PostMessage_NonOKStatusIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer srv.Close()
	n := notify.NewSlackNotifier(notify.Config{WebhookURL: srv.URL})

	err := n.PostMessage(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_payload") {
		t.Errorf("error = %v, want it to mention the response body", err)
	}
}

func TestSlackNotifier_KillSwitchTriggered_AutoResumable(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	ev := domain.KillSwitchEvent{
		Reason:      domain.KillReasonMarketDataDown,
		TriggeredAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
		DetailJSON:  `{"stale_seconds":120}`,
	}
	if err := n.KillSwitchTriggered(context.Background(), ev, true); err != nil {
		t.Fatalf("KillSwitchTriggered: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	msg := got[0]
	for _, want := range []string{"市場データ停止", "market_data_down", "自動再開", `{"stale_seconds":120}`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "手動再開が必要です") {
		t.Errorf("auto-resumable message should not claim manual resume is required: %q", msg)
	}
}

func TestSlackNotifier_KillSwitchTriggered_ManualResumeOnly(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	ev := domain.KillSwitchEvent{
		Reason:      domain.KillReasonUnexpectedPosition,
		TriggeredAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
		DetailJSON:  `{}`,
	}
	if err := n.KillSwitchTriggered(context.Background(), ev, false); err != nil {
		t.Fatalf("KillSwitchTriggered: %v", err)
	}

	msg := got[0]
	if !strings.Contains(msg, "想定外ポジション検知") {
		t.Errorf("message %q does not contain the reason label", msg)
	}
	if !strings.Contains(msg, "手動再開が必要です") {
		t.Errorf("manual-only message should state manual resume is required: %q", msg)
	}
}

func TestSlackNotifier_KillSwitchAutoResumed(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	ev := domain.KillSwitchEvent{
		Reason:      domain.KillReasonJevAPIDown,
		TriggeredAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
	}
	if err := n.KillSwitchAutoResumed(context.Background(), ev); err != nil {
		t.Fatalf("KillSwitchAutoResumed: %v", err)
	}
	if !strings.Contains(got[0], "自動再開") || !strings.Contains(got[0], "jev_api_down") {
		t.Errorf("message %q missing expected content", got[0])
	}
}

func TestSlackNotifier_DailyLossWarning(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	if err := n.DailyLossWarning(context.Background(), 0.024, 0.03); err != nil {
		t.Fatalf("DailyLossWarning: %v", err)
	}
	if !strings.Contains(got[0], "日次損失上限接近") {
		t.Errorf("message %q missing expected content", got[0])
	}
}

func TestSlackNotifier_JevAPIErrorRateExceeded(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	if err := n.JevAPIErrorRateExceeded(context.Background(), 0.6, 0.5); err != nil {
		t.Fatalf("JevAPIErrorRateExceeded: %v", err)
	}
	if !strings.Contains(got[0], "Jev APIエラー率上昇") {
		t.Errorf("message %q missing expected content", got[0])
	}
}

func TestSlackNotifier_ProposalApplied(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	proposal := domain.PolicyProposal{ID: 42, AppliedPolicyVersion: ptr("sol-42")}
	if err := n.ProposalApplied(context.Background(), proposal); err != nil {
		t.Fatalf("ProposalApplied: %v", err)
	}
	if !strings.Contains(got[0], "sol-42") {
		t.Errorf("message %q missing expected content", got[0])
	}
}

func TestSlackNotifier_ProposalRolledBack(t *testing.T) {
	var got []string
	n, closeSrv := captureWebhook(t, &got)
	defer closeSrv()

	proposal := domain.PolicyProposal{ID: 42}
	if err := n.ProposalRolledBack(context.Background(), proposal, "expectancy degraded 25%"); err != nil {
		t.Fatalf("ProposalRolledBack: %v", err)
	}
	if !strings.Contains(got[0], "expectancy degraded 25%") {
		t.Errorf("message %q missing expected content", got[0])
	}
}

func ptr[T any](v T) *T { return &v }
