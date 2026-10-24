package system_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// fakeTokenStatus is a broker session whose status is the kabu adapter's
// mapping of a token issuance outcome, so the guidance texts asserted below
// are the kabu adapter's own (issues #295, #712).
type fakeTokenStatus marketdata.TokenStatus

func (f fakeTokenStatus) Status() broker.SessionStatus {
	return kabu.SessionStatusOf(marketdata.TokenStatus(f))
}

func TestMarketDataHandler_BannerProps(t *testing.T) {
	tests := []struct {
		name           string
		source         system.MarketDataStatusSource
		wantIssue      string
		wantGuidance   []string
		notWantInGuide []string
		wantPersistent bool
		wantNone       bool
	}{
		{name: "token ok has nothing to report", source: fakeTokenStatus{}, wantNone: true},
		{name: "nil source has nothing to report", source: nil, wantNone: true},
		{
			name:         "bad password",
			source:       fakeTokenStatus{Issue: marketdata.TokenIssueBadPassword, Code: 4001013},
			wantIssue:    "bad_password",
			wantGuidance: []string{"KABU_API_PASSWORD"},
		},
		{
			name:         "kabu station unreachable",
			source:       fakeTokenStatus{Issue: marketdata.TokenIssueUnreachable},
			wantIssue:    "unreachable",
			wantGuidance: []string{"起動"},
		},
		{
			name:           "not logged in right after the first failure is not escalated",
			source:         fakeTokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001007, Failures: 1, Since: time.Now()},
			wantIssue:      "not_logged_in",
			wantGuidance:   []string{"ログアウト"},
			wantPersistent: false,
		},
		{
			name:           "persistent not logged in is escalated",
			source:         fakeTokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001017, Failures: 7, Since: time.Now().Add(-12 * time.Minute)},
			wantIssue:      "not_logged_in",
			wantGuidance:   []string{"ログアウト"},
			wantPersistent: true,
			// issue #305: login-only guidance, never steering to the API password / 「APIを利用する」.
			notWantInGuide: []string{"KABU_API_PASSWORD", "APIシステム設定", "起動していること"},
		},
		{
			name:           "persistent escalation is only for not logged in",
			source:         fakeTokenStatus{Issue: marketdata.TokenIssueBadPassword, Code: 4001013, Failures: 9, Since: time.Now().Add(-time.Hour)},
			wantIssue:      "bad_password",
			wantGuidance:   []string{"KABU_API_PASSWORD"},
			wantPersistent: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := system.NewMarketDataHandler(tt.source).BannerProps(context.Background())
			if tt.wantNone {
				if got != (organisms.MarketDataBannerProps{}) {
					t.Fatalf("props = %+v, want the zero value", got)
				}
				return
			}
			if got.Issue != tt.wantIssue || got.Persistent != tt.wantPersistent {
				t.Errorf("issue/persistent = %q/%v, want %q/%v", got.Issue, got.Persistent, tt.wantIssue, tt.wantPersistent)
			}
			for _, w := range tt.wantGuidance {
				if !strings.Contains(got.Guidance, w) {
					t.Errorf("guidance missing %q: %s", w, got.Guidance)
				}
			}
			for _, w := range tt.notWantInGuide {
				if strings.Contains(got.Guidance, w) {
					t.Errorf("guidance must not contain %q: %s", w, got.Guidance)
				}
			}
		})
	}
}

type fixedStatus broker.SessionStatus

func (f fixedStatus) Status() broker.SessionStatus { return broker.SessionStatus(f) }

// Issue #727: the 立花 adapter's version heads-up is a notice, not an error; an
// error (or the nightly closed hours) keeps the error guidance.
func TestMarketDataHandler_BannerProps_BrokerNotices(t *testing.T) {
	tests := []struct {
		name         string
		status       broker.SessionStatus
		wantGuidance bool
		wantNotice   string
		wantIssue    string
	}{
		{
			name:       "version retiring while the session is fine is only a notice",
			status:     broker.SessionStatus{VersionRetiring: true},
			wantNotice: "リリース予定日",
		},
		{
			name:         "an error is reported next to the notice (the consumer ranks the error first)",
			status:       broker.SessionStatus{Issue: broker.SessionIssueUnreachable, Guidance: "接続できません。", VersionRetiring: true},
			wantGuidance: true,
			wantIssue:    "unreachable",
			wantNotice:   "リリース予定日",
		},
		{
			name:         "nightly closed hours carry the schedule guidance",
			status:       broker.SessionStatus{Issue: broker.SessionIssueOutOfHours, Guidance: "開局後（05:35）に自動で再ログインします。"},
			wantGuidance: true,
			wantIssue:    "out_of_hours",
		},
		{name: "nothing to say reports nothing", status: broker.SessionStatus{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := system.NewMarketDataHandler(fixedStatus(tt.status)).BannerProps(context.Background())
			if (got.Guidance != "") != tt.wantGuidance || got.Issue != tt.wantIssue {
				t.Errorf("issue/guidance = %q/%q, want issue %q with guidance %v", got.Issue, got.Guidance, tt.wantIssue, tt.wantGuidance)
			}
			if (tt.wantNotice == "") != (got.Notice == "") || !strings.Contains(got.Notice, tt.wantNotice) {
				t.Errorf("notice = %q, want it to contain %q", got.Notice, tt.wantNotice)
			}
		})
	}
}

type fakeWatchNotice string

func (f fakeWatchNotice) WatchNotice(context.Context) string { return string(f) }

// Issue #730: a stand-in 立花 watch list (the daily bars were unusable) is a
// notice, joined to the version heads-up.
func TestMarketDataHandler_BannerProps_WatchListNotice(t *testing.T) {
	const standIn = "監視リストを前営業日から引き継ぎました。"
	props := func(status broker.SessionStatus, notice string) organisms.MarketDataBannerProps {
		return system.NewMarketDataHandler(fixedStatus(status)).WithWatchNotice(fakeWatchNotice(notice)).BannerProps(context.Background())
	}

	if got := props(broker.SessionStatus{}, standIn); got.Notice != standIn {
		t.Errorf("notice = %q, want %q", got.Notice, standIn)
	}
	if got := props(broker.SessionStatus{VersionRetiring: true}, standIn); !strings.Contains(got.Notice, "リリース予定日") || !strings.Contains(got.Notice, standIn) {
		t.Errorf("both notices must be reported: %q", got.Notice)
	}
	if got := props(broker.SessionStatus{}, ""); got.Notice != "" {
		t.Errorf("no notice expected: %q", got.Notice)
	}
	if got := props(broker.SessionStatus{Issue: broker.SessionIssueUnreachable, Guidance: "接続できません。"}, standIn); got.Guidance == "" {
		t.Errorf("the failure must still be reported next to the notice: %+v", got)
	}
}
