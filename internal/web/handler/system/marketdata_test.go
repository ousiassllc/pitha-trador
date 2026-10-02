package system_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
)

type fakeTokenStatus marketdata.TokenStatus

func (f fakeTokenStatus) TokenStatus() marketdata.TokenStatus { return marketdata.TokenStatus(f) }

func TestMarketDataHandler_Status(t *testing.T) {
	tests := []struct {
		name     string
		source   system.MarketDataStatusSource
		want     []string
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
		})
	}
}
