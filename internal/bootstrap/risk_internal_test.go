package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// recordingRiskNotifier stands in for cmd/desktop's Wails App: the
// entrypoint-specific extra risk.Notifier BuildServices fans out to.
type recordingRiskNotifier struct {
	risk.NoopNotifier
	triggered []string
}

func (n *recordingRiskNotifier) KillSwitchTriggered(_ context.Context, ev domain.KillSwitchEvent, _ bool) error {
	n.triggered = append(n.triggered, ev.Reason)
	return nil
}

func buildServicesWithSecrets(t *testing.T, secrets config.Secrets, notifiers ...risk.Notifier) *Services {
	t.Helper()
	state, err := Run(Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })

	svc, err := BuildServices(state, secrets, nil, notifiers...)
	if err != nil {
		t.Fatalf("BuildServices: %v", err)
	}
	return svc
}

func TestBuildServices_KillSwitchFansOutToSlackAndExtraNotifiers(t *testing.T) {
	var slackPosts atomic.Int32
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		slackPosts.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(slack.Close)

	desktop := &recordingRiskNotifier{}
	svc := buildServicesWithSecrets(t, config.Secrets{SlackWebhookURL: slack.URL}, desktop)

	if _, err := svc.Risk.TriggerKillSwitch(context.Background(), domain.KillReasonUnexpectedPosition, nil); err != nil {
		t.Fatalf("TriggerKillSwitch: %v", err)
	}

	if got := slackPosts.Load(); got != 1 {
		t.Errorf("Slack webhook posts = %d, want 1", got)
	}
	if len(desktop.triggered) != 1 || desktop.triggered[0] != domain.KillReasonUnexpectedPosition {
		t.Errorf("extra notifier KillSwitchTriggered calls = %v, want [%s]", desktop.triggered, domain.KillReasonUnexpectedPosition)
	}
}

func TestBuildServices_RiskEngineStateReflectsKill(t *testing.T) {
	svc := buildServicesWithSecrets(t, config.Secrets{})
	ctx := context.Background()

	if err := svc.Risk.Kill(ctx); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	state, _, err := svc.Risk.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != domain.SystemStateKilled {
		t.Errorf("State after Kill = %q, want %q", state, domain.SystemStateKilled)
	}
	if passed, reason := svc.Risk.Check(ctx, 1, domain.JevDirectionLong); passed || !strings.HasPrefix(reason, risk.ReasonKillSwitchActive) {
		t.Errorf("Check after Kill = (%v, %q), want (false, %q...)", passed, reason, risk.ReasonKillSwitchActive)
	}
}
