package analysisflow_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler"
)

type failingProposalSource struct{}

func (failingProposalSource) List(context.Context, string, int) ([]domain.PolicyProposal, error) {
	return nil, errors.New("db down")
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &logs
}

func healthyProposal(id int64) domain.PolicyProposal {
	return domain.PolicyProposal{
		ID: id, ProposedAt: time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC), ProposedBy: "sol", Status: domain.PolicyProposalStatusApplied,
		ProposedChangesJSON: `[{"key":"policy.long.min_probability","old_value":"0.6","new_value":"0.68"}]`,
		BacktestResultJSON:  strPtr(`{"baseline_expectancy":0.10,"candidate_expectancy":0.12,"baseline_max_drawdown_pct":5,"candidate_max_drawdown_pct":4}`),
		ReviewJSON:          strPtr(`{"verdict":"approve"}`),
	}
}

// One undecodable row must not hide the rest of the audit history: the row
// is still returned with only the broken field emptied, and its proposal_id
// is logged.
func TestNew_APIPolicyProposalsCorruptRowDoesNotHideOtherRows(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(p *domain.PolicyProposal)
		want    func(item map[string]any) string // "" when the item is as expected
	}{
		{
			name:    "invalid proposed_changes JSON",
			corrupt: func(p *domain.PolicyProposal) { p.ProposedChangesJSON = "{not json" },
			want: func(item map[string]any) string {
				if m, ok := item["proposed_changes"].(map[string]any); !ok || len(m) != 0 {
					return "proposed_changes must be {}"
				}
				if item["backtest_result"] == nil || item["review"] == nil {
					return "valid backtest_result/review must be kept"
				}
				return ""
			},
		},
		{
			name:    "non-JSON new_value",
			corrupt: func(p *domain.PolicyProposal) { p.ProposedChangesJSON = `[{"key":"policy.x","new_value":"abc"}]` },
			want: func(item map[string]any) string {
				if m, ok := item["proposed_changes"].(map[string]any); !ok || len(m) != 0 {
					return "proposed_changes must be {}"
				}
				return ""
			},
		},
		{
			name:    "invalid backtest_result JSON",
			corrupt: func(p *domain.PolicyProposal) { p.BacktestResultJSON = strPtr("{broken") },
			want: func(item map[string]any) string {
				if item["backtest_result"] != nil {
					return "backtest_result must be null"
				}
				if m, ok := item["proposed_changes"].(map[string]any); !ok || len(m) != 1 {
					return "valid proposed_changes must be kept"
				}
				return ""
			},
		},
		{
			name:    "invalid review JSON",
			corrupt: func(p *domain.PolicyProposal) { p.ReviewJSON = strPtr("{broken") },
			want: func(item map[string]any) string {
				if item["review"] != nil {
					return "review must be null"
				}
				return ""
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			logs := captureLogs(t)
			bad := healthyProposal(41)
			tc.corrupt(&bad)
			src := handler.StaticPolicyProposalSource{Proposals: []domain.PolicyProposal{healthyProposal(42), bad, healthyProposal(40)}}
			engine := router.New(router.WithPolicyProposalSource(src))

			code, body := getProposals(t, engine, "")
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %v)", code, body)
			}
			items := body["items"].([]any)
			if len(items) != 3 {
				t.Fatalf("items = %d, want 3 (corrupt row kept, others returned)", len(items))
			}
			for i, id := range []float64{42, 41, 40} {
				if got := items[i].(map[string]any)["id"]; got != id {
					t.Errorf("items[%d].id = %v, want %v", i, got, id)
				}
			}
			if msg := tc.want(items[1].(map[string]any)); msg != "" {
				t.Errorf("corrupt row: %s: %v", msg, items[1])
			}
			if !strings.Contains(logs.String(), "proposal_id=41") {
				t.Errorf("corrupt row id not logged: %q", logs.String())
			}
			if strings.Contains(logs.String(), "proposal_id=42") || strings.Contains(logs.String(), "proposal_id=40") {
				t.Errorf("healthy rows must not be logged: %q", logs.String())
			}
		})
	}
}

func TestNew_APIPolicyProposalsStoreFailureIs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithPolicyProposalSource(failingProposalSource{}))
	if code, _ := getProposals(t, engine, ""); code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a store failure", code)
	}
}
