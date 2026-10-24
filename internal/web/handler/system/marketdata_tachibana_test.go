package system_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
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

func bannerProps(source system.MarketDataStatusSource, selection system.BrokerSelection) organisms.MarketDataBannerProps {
	h := system.NewMarketDataHandler(source)
	if selection != nil {
		h.WithBrokerSelection(selection)
	}
	return h.BannerProps(context.Background())
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
			got := bannerProps(fakeSession{Issue: tt.issue, Failures: 1, Since: time.Now()}, tachibanaSelected(config.TachibanaEnvDemo))
			if got.Issue != string(tt.issue) || got.Environment != config.TachibanaEnvDemo {
				t.Errorf("issue/environment = %q/%q, want %q/demo", got.Issue, got.Environment, tt.issue)
			}
			for _, want := range tt.want {
				if !strings.Contains(got.Guidance, want) {
					t.Errorf("guidance lacks %q: %s", want, got.Guidance)
				}
			}
			if got.Persistent || strings.Contains(got.Guidance, "KABU_API_PASSWORD") || strings.Contains(got.Guidance, "kabuステーション") {
				t.Errorf("kabu-specific content in the 立花 props: %+v", got)
			}
			if prev, dup := seen[got.Guidance]; dup {
				t.Errorf("same guidance for %s and %s", prev, tt.issue)
			}
			seen[got.Guidance] = tt.issue
		})
	}
}

// issue #739: the props always name the environment.
func TestMarketDataHandler_TachibanaEnvironmentAlwaysShown(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		want        string
	}{
		{"demo", config.TachibanaEnvDemo, config.TachibanaEnvDemo},
		{"production", config.TachibanaEnvProduction, config.TachibanaEnvProduction},
		{"empty falls back to the demo default", "", config.TachibanaEnvDemo},
	}
	for _, tt := range tests {
		for _, issue := range []broker.SessionIssue{broker.SessionIssueBadAuthID, broker.SessionIssueClockSkew, broker.SessionIssueUnknown, broker.SessionIssueNotLoggedIn + "-unlisted"} {
			t.Run(tt.name+"/"+string(issue), func(t *testing.T) {
				got := bannerProps(fakeSession{Issue: issue}, tachibanaSelected(tt.environment))
				if got.Environment != tt.want {
					t.Errorf("environment = %q, want %q", got.Environment, tt.want)
				}
			})
		}
	}
	prod := bannerProps(fakeSession{Issue: broker.SessionIssueBadAuthID}, tachibanaSelected(config.TachibanaEnvProduction))
	if !strings.Contains(prod.Guidance, "本番環境の認証ID") {
		t.Errorf("production guidance names the wrong environment: %s", prod.Guidance)
	}
}

// issue #739: nothing credential-like reaches the props, even when an adapter
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
		got := bannerProps(fakeSession{Issue: issue, Guidance: leaky, Failures: 9, Since: time.Now().Add(-time.Hour)}, tachibanaSelected(config.TachibanaEnvProduction))
		shown := got.Guidance + " " + got.Notice
		for _, secret := range []string{authID, keyBody, second, "VIRTUALURLTOKEN", "e-shiten.jp"} {
			if strings.Contains(shown, secret) {
				t.Errorf("issue %s: props contain %q: %+v", issue, secret, got)
			}
		}
	}
}

// 立花 failures never escalate to the kabu "log in to kabuステーション" form.
func TestMarketDataHandler_TachibanaIgnoresKabuPersistent(t *testing.T) {
	got := bannerProps(fakeSession{Issue: broker.SessionIssueSessionConflict, Failures: 9, Since: time.Now().Add(-time.Hour)}, tachibanaSelected(config.TachibanaEnvDemo))
	if got.Persistent || strings.Contains(got.Guidance, "kabuステーション") {
		t.Errorf("kabu escalation in the 立花 props: %+v", got)
	}
}

// 立花 selected but a kabu adapter is the one running (not_logged_in /
// bad_password are kabu-only): the kabu content stands, with no environment.
func TestMarketDataHandler_KabuOnlyCausesKeepKabuContentUnderTachibanaSelection(t *testing.T) {
	for _, status := range []fakeTokenStatus{
		{Issue: marketdata.TokenIssueBadPassword, Code: 4001013},
		{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001017, Failures: 7, Since: time.Now().Add(-12 * time.Minute)},
	} {
		want := bannerProps(status, nil)
		got := bannerProps(status, tachibanaSelected(config.TachibanaEnvDemo))
		if got != want || got.Environment != "" {
			t.Errorf("issue %s: kabu content changed:\n got %+v\nwant %+v", status.Issue, got, want)
		}
	}
}

// issue #739 regression: with kabu selected (explicitly, by default, or when
// the selection cannot be read) the content is exactly the kabu one.
func TestMarketDataHandler_KabuSelectionContentUnchanged(t *testing.T) {
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
		want := bannerProps(status, nil)
		for name, selection := range selections {
			got := bannerProps(status, selection)
			if got != want {
				t.Errorf("%s / %s: content changed:\n got %+v\nwant %+v", name, status.Issue, got, want)
			}
			if got.Environment != "" || strings.Contains(got.Guidance, "立花") {
				t.Errorf("%s / %s: 立花 content in the kabu props: %+v", name, status.Issue, got)
			}
		}
	}
}

// A healthy 立花 session reports nothing.
func TestMarketDataHandler_TachibanaHealthyReportsNothing(t *testing.T) {
	if got := bannerProps(fakeSession{}, tachibanaSelected(config.TachibanaEnvDemo)); got != (organisms.MarketDataBannerProps{}) {
		t.Errorf("healthy props = %+v, want the zero value", got)
	}
}
