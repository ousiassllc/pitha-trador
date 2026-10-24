package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

type failingSession struct{ issue broker.SessionIssue }

func (f failingSession) Status() broker.SessionStatus {
	return broker.SessionStatus{Issue: f.issue, Guidance: "kabuステーションを起動してください。"}
}

// brokerSelectionOps is the OperationalSettings stand-in for the broker selection the Home 要対応 list follows.
type brokerSelectionOps struct{ selected config.BrokerSettings }

func (brokerSelectionOps) Get(context.Context, string) (v opsettings.Value, _ error) { return }
func (brokerSelectionOps) Save(context.Context, string, string) error                { return nil }
func (brokerSelectionOps) Reset(context.Context, string) error                       { return nil }
func (b brokerSelectionOps) Broker(context.Context) (config.BrokerSettings, error) {
	return b.selected, nil
}
func (brokerSelectionOps) DailyGuard(context.Context) (dailyguardsettings.Resolved, error) {
	return dailyguardsettings.Resolved{}, nil
}

// issue #739: Home's 要対応 list follows the saved broker selection.
func TestNew_HomeAttentionFollowsBrokerSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prod := config.BrokerSettings{Provider: config.BrokerTachibana, Tachibana: config.TachibanaSettings{Environment: config.TachibanaEnvProduction}}
	tests := []struct {
		name     string
		selected config.BrokerSettings
		want     []string
	}{
		{"tachibana production", prod, []string{"立花証券", "時計", "p_errno=8"}},
		{"kabu", config.BrokerSettings{Provider: config.BrokerKabu}, []string{"kabuステーション"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := broker.SessionIssueClockSkew
			if tt.selected.Provider == config.BrokerKabu {
				issue = broker.SessionIssueUnreachable
			}
			engine := router.New(
				router.WithMarketDataStatus(failingSession{issue: issue}),
				router.WithOperationalSettings(brokerSelectionOps{selected: tt.selected}),
			)
			req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/home/attention", nil))
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			for _, want := range tt.want {
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("body lacks %q: %s", want, rec.Body.String())
				}
			}
			if tt.selected.Provider == config.BrokerKabu && strings.Contains(rec.Body.String(), "立花") {
				t.Errorf("kabu item names 立花: %s", rec.Body.String())
			}
		})
	}
}
