package system_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

type fakeSession broker.SessionStatus

func (f fakeSession) Status() broker.SessionStatus { return broker.SessionStatus(f) }

type fakeSelection struct {
	settings config.BrokerSettings
	err      error
}

func (f fakeSelection) Broker(context.Context) (config.BrokerSettings, error) {
	return f.settings, f.err
}

func tachibanaSelected(environment string) fakeSelection {
	return fakeSelection{settings: config.BrokerSettings{
		Provider:  config.BrokerTachibana,
		Tachibana: config.TachibanaSettings{Environment: environment},
	}}
}

func renderBanner(t *testing.T, source system.MarketDataStatusSource, selection system.BrokerSelection) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := system.NewMarketDataHandler(source)
	if selection != nil {
		h.WithBrokerSelection(selection)
	}
	engine.GET("/system/marketdata-status", h.Status)
	rec := serve(engine, http.MethodGet, "/system/marketdata-status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// issue #739: every 立花 cause gets its own guidance text.
func TestMarketDataHandler_TachibanaCauses(t *testing.T) {
	tests := []struct {
		issue broker.SessionIssue
		want  []string
	}{
		{broker.SessionIssueBadAuthID, []string{"認証IDが正しくありません", "デモ環境の認証ID", "別セット"}},
		{broker.SessionIssueKeyMismatch, []string{"秘密鍵を使えません", "公開鍵", "復号"}},
		{broker.SessionIssueAPIDisabled, []string{"API利用設定が「利用しない」", "「利用する」"}},
		{broker.SessionIssueDocumentsUnread, []string{"未読の書面", "既読"}},
		{broker.SessionIssueIPRejected, []string{"10005", "IPv6", "固定IP"}},
		{broker.SessionIssueClockSkew, []string{"p_errno=8", "30秒", "NTP"}},
		{broker.SessionIssueOutOfHours, []string{"サービス時間外", "03:30〜05:30", "05:30以降"}},
		{broker.SessionIssueSessionConflict, []string{"多重ログイン", "二重起動"}},
		{broker.SessionIssueUnreachable, []string{"接続できません", "IPv4"}},
		{broker.SessionIssueRejected, []string{"拒否されています"}},
		{broker.SessionIssueUnknown, []string{"原因を特定できない", "エラーログ"}},
	}
	seen := map[string]broker.SessionIssue{}
	for _, tt := range tests {
		t.Run(string(tt.issue), func(t *testing.T) {
			body := renderBanner(t, fakeSession{Issue: tt.issue, Failures: 1, Since: time.Now()}, tachibanaSelected(config.TachibanaEnvDemo))
			common := []string{`data-testid="marketdata-banner"`, `data-issue="` + string(tt.issue) + `"`, "市況データを取得できません。", "自動的に再試行します。", `href="/settings"`}
			for _, want := range append(common, tt.want...) {
				if !strings.Contains(body, want) {
					t.Errorf("banner lacks %q:\n%s", want, body)
				}
			}
			if strings.Contains(body, "data-persistent") || strings.Contains(body, "KABU_API_PASSWORD") || strings.Contains(body, "kabuステーション") {
				t.Errorf("kabu-specific text in the 立花 banner:\n%s", body)
			}
			// data-issue differs per cause, so compare the guidance only.
			guidance := body[strings.Index(body, "</span>")+len("</span>"):]
			if prev, dup := seen[guidance]; dup {
				t.Errorf("same guidance for %s and %s", prev, tt.issue)
			}
			seen[guidance] = tt.issue
		})
	}
}

// issue #739: the banner always names the environment.
func TestMarketDataHandler_TachibanaEnvironmentAlwaysShown(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		want        string
		notWant     string
		badge       string
	}{
		{"demo", config.TachibanaEnvDemo, `data-environment="demo"`, `data-environment="production"`, "立花証券 デモ環境"},
		{"production", config.TachibanaEnvProduction, `data-environment="production"`, `data-environment="demo"`, "立花証券 本番環境"},
		{"empty falls back to the demo default", "", `data-environment="demo"`, `data-environment="production"`, "立花証券 デモ環境"},
	}
	for _, tt := range tests {
		for _, issue := range []broker.SessionIssue{broker.SessionIssueBadAuthID, broker.SessionIssueClockSkew, broker.SessionIssueUnknown, broker.SessionIssueNotLoggedIn + "-unlisted"} {
			t.Run(tt.name+"/"+string(issue), func(t *testing.T) {
				body := renderBanner(t, fakeSession{Issue: issue}, tachibanaSelected(tt.environment))
				for _, want := range []string{tt.want, tt.badge} {
					if !strings.Contains(body, want) {
						t.Errorf("banner lacks %q:\n%s", want, body)
					}
				}
				if strings.Contains(body, tt.notWant) {
					t.Errorf("banner has %q:\n%s", tt.notWant, body)
				}
			})
		}
	}
	prod := renderBanner(t, fakeSession{Issue: broker.SessionIssueBadAuthID}, tachibanaSelected(config.TachibanaEnvProduction))
	if !strings.Contains(prod, "本番環境の認証ID") {
		t.Errorf("production guidance names the wrong environment: %s", prod)
	}
}

// issue #739: nothing credential-like reaches the HTML, even when an adapter
// (wrongly) puts it in SessionStatus.Guidance.
func TestMarketDataHandler_TachibanaNeverLeaksSecrets(t *testing.T) {
	const (
		authID  = "AUTH-ID-SECRET"
		keyBody = "PRIVATE-KEY-BODY-SECRET"
		second  = "SECOND-PASSWORD-SECRET"
		vurl    = "https://kabuka.e-shiten.jp/e_api_v4r10/request/VIRTUALURLTOKEN123456/"
	)
	leaky := authID + " " + keyBody + " " + second + " " + vurl
	for _, issue := range []broker.SessionIssue{
		broker.SessionIssueBadAuthID, broker.SessionIssueKeyMismatch, broker.SessionIssueAPIDisabled, broker.SessionIssueDocumentsUnread,
		broker.SessionIssueIPRejected, broker.SessionIssueClockSkew, broker.SessionIssueOutOfHours, broker.SessionIssueSessionConflict,
		broker.SessionIssueUnreachable, broker.SessionIssueRejected, broker.SessionIssueUnknown,
	} {
		body := renderBanner(t, fakeSession{Issue: issue, Guidance: leaky, Failures: 9, Since: time.Now().Add(-time.Hour)}, tachibanaSelected(config.TachibanaEnvProduction))
		for _, secret := range []string{authID, keyBody, second, "VIRTUALURLTOKEN", "e-shiten.jp"} {
			if strings.Contains(body, secret) {
				t.Errorf("issue %s: banner contains %q:\n%s", issue, secret, body)
			}
		}
	}
}

// 立花 failures never escalate to the kabu "log in to kabuステーション" form.
func TestMarketDataHandler_TachibanaIgnoresKabuPersistent(t *testing.T) {
	body := renderBanner(t, fakeSession{Issue: broker.SessionIssueSessionConflict, Failures: 9, Since: time.Now().Add(-time.Hour)}, tachibanaSelected(config.TachibanaEnvDemo))
	if strings.Contains(body, "data-persistent") || strings.Contains(body, "kabuステーション") {
		t.Errorf("kabu escalation in the 立花 banner:\n%s", body)
	}
}

// 立花 selected but a kabu adapter is the one running (not_logged_in /
// bad_password are kabu-only): the kabu banner stands, with no environment.
func TestMarketDataHandler_KabuOnlyCausesKeepKabuBannerUnderTachibanaSelection(t *testing.T) {
	for _, status := range []fakeTokenStatus{
		{Issue: marketdata.TokenIssueBadPassword, Code: 4001013},
		{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001017, Failures: 7, Since: time.Now().Add(-12 * time.Minute)},
	} {
		want := renderBanner(t, status, nil)
		got := renderBanner(t, status, tachibanaSelected(config.TachibanaEnvDemo))
		if got != want || strings.Contains(got, "data-environment") {
			t.Errorf("issue %s: kabu banner changed:\n got %s\nwant %s", status.Issue, got, want)
		}
	}
}

// issue #739 regression: with kabu selected (explicitly, by default, or when
// the selection cannot be read) the banner is byte-for-byte the kabu one.
func TestMarketDataHandler_KabuSelectionBannerUnchanged(t *testing.T) {
	selections := map[string]system.BrokerSelection{
		"kabu":        fakeSelection{settings: config.BrokerSettings{Provider: config.BrokerKabu}},
		"zero value":  fakeSelection{},
		"read failed": fakeSelection{err: errors.New("db down")},
	}
	for _, status := range []fakeTokenStatus{
		{},
		{Issue: marketdata.TokenIssueBadPassword, Code: 4001013},
		{Issue: marketdata.TokenIssueUnreachable},
		{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001007, Failures: 1, Since: time.Now()},
		{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001017, Failures: 7, Since: time.Now().Add(-12 * time.Minute)},
	} {
		want := renderBanner(t, status, nil)
		for name, selection := range selections {
			got := renderBanner(t, status, selection)
			if got != want {
				t.Errorf("%s / %s: banner changed:\n got %s\nwant %s", name, status.Issue, got, want)
			}
			if strings.Contains(got, "data-environment") || strings.Contains(got, "立花") {
				t.Errorf("%s / %s: 立花 text in the kabu banner:\n%s", name, status.Issue, got)
			}
		}
	}
}

// A healthy 立花 session renders nothing.
func TestMarketDataHandler_TachibanaHealthyRendersNothing(t *testing.T) {
	if body := renderBanner(t, fakeSession{}, tachibanaSelected(config.TachibanaEnvDemo)); strings.TrimSpace(body) != "" {
		t.Errorf("healthy banner = %q, want empty", body)
	}
}
