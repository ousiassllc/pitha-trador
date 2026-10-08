package system_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

// fakeTokenStatus is a broker session whose status is the kabu adapter's
// mapping of a token issuance outcome, so the banner texts asserted below are
// the kabu adapter's own guidance (issues #295, #712).
type fakeTokenStatus marketdata.TokenStatus

func (f fakeTokenStatus) Status() broker.SessionStatus {
	return kabu.SessionStatusOf(marketdata.TokenStatus(f))
}

func TestMarketDataHandler_Status(t *testing.T) {
	tests := []struct {
		name     string
		source   system.MarketDataStatusSource
		want     []string
		notWant  []string
		wantNone bool
	}{
		{name: "token ok renders nothing", source: fakeTokenStatus{}, wantNone: true},
		{name: "nil source renders nothing", source: nil, wantNone: true},
		{
			name:   "bad password",
			source: fakeTokenStatus{Issue: marketdata.TokenIssueBadPassword, Code: 4001013},
			want:   []string{`data-testid="marketdata-banner"`, `data-issue="bad_password"`, "KABU_API_PASSWORD", `href="/settings"`},
		},
		{
			name:   "kabu station unreachable",
			source: fakeTokenStatus{Issue: marketdata.TokenIssueUnreachable},
			want:   []string{`data-issue="unreachable"`, "起動"},
		},
		{
			name:    "not logged in right after the first failure is the ordinary banner",
			source:  fakeTokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001007, Failures: 1, Since: time.Now()},
			want:    []string{`data-issue="not_logged_in"`, "ログアウト", "自動的に再試行します。", `href="/settings"`},
			notWant: []string{"data-persistent", "お待ちください"},
		},
		{
			name:   "persistent not logged in escalates with elapsed time and failure count",
			source: fakeTokenStatus{Issue: marketdata.TokenIssueNotLoggedIn, Code: 4001017, Failures: 7, Since: time.Now().Add(-12 * time.Minute)},
			want: []string{
				`data-testid="marketdata-banner"`, `data-issue="not_logged_in"`, `data-persistent="true"`,
				"未ログインの状態が続いています", "10分以上継続", "失敗 7回", "4001007 / 4001017",
				"ログアウト", "再ログインしてから、そのままお待ちください", "二重起動", "/token",
			},
			// issue #305: login-only guidance, never steering to the API password / 「APIを利用する」.
			notWant: []string{"KABU_API_PASSWORD", "APIシステム設定", "起動していること", `href="/settings"`},
		},
		{
			name:    "persistent escalation is only for not logged in",
			source:  fakeTokenStatus{Issue: marketdata.TokenIssueBadPassword, Code: 4001013, Failures: 9, Since: time.Now().Add(-time.Hour)},
			want:    []string{`data-issue="bad_password"`, "KABU_API_PASSWORD"},
			notWant: []string{"data-persistent"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			engine.GET("/system/marketdata-status", system.NewMarketDataHandler(tt.source).Status)

			rec := serve(engine, http.MethodGet, "/system/marketdata-status")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if tt.wantNone && strings.Contains(body, "marketdata-banner") {
				t.Errorf("body = %q, want no banner", body)
			}
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body missing %q:\n%s", w, body)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(body, w) {
					t.Errorf("body must not contain %q:\n%s", w, body)
				}
			}
		})
	}
}
