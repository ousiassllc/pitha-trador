// Package analysisflow_test holds the analysis-route (policy proposals) tests
// of router. They only use router's exported API and live in their own
// directory to keep internal/router under the linterly line budget (#248).
package analysisflow_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/proposals"
)

func strPtr(s string) *string { return &s }

func proposalFixtures(t *testing.T) []domain.PolicyProposal {
	t.Helper()
	applied, err := domain.EncodePolicyChanges([]domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.6", NewValue: "0.68"}})
	if err != nil {
		t.Fatal(err)
	}
	return []domain.PolicyProposal{
		{
			ID: 42, ProposedAt: time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC), ProposedBy: "sol", Status: domain.PolicyProposalStatusApplied,
			ProposedChangesJSON:  applied,
			BacktestResultJSON:   strPtr(`{"baseline_expectancy":0.10,"candidate_expectancy":0.12,"baseline_max_drawdown_pct":5,"candidate_max_drawdown_pct":4}`),
			ReviewedBy:           strPtr("opus"),
			ReviewJSON:           strPtr(`{"verdict":"approve","reason":"sound"}`),
			AppliedPolicyVersion: strPtr("v12"),
		},
		{
			ID: 41, ProposedAt: time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC), ProposedBy: "sol", Status: domain.PolicyProposalStatusRejected,
			ProposedChangesJSON: `[{"key":"risk.max_position_size","old_value":"1","new_value":"2"}]`,
			ReviewedBy:          strPtr("governor"),
			ReviewJSON:          strPtr(`{"verdict":"reject","reason":"llm_output_out_of_bounds"}`),
		},
	}
}

func getProposals(t *testing.T, engine *gin.Engine, query string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-proposals"+query, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

func TestNew_APIPolicyProposalsReturnsAuditHistoryInSpecFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithPolicyProposalSource(proposals.StaticPolicyProposalSource{Proposals: proposalFixtures(t)}))

	code, body := getProposals(t, engine, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}

	applied := items[0].(map[string]any)
	if applied["id"] != float64(42) || applied["proposed_by"] != "sol" || applied["status"] != "applied" ||
		applied["reviewed_by"] != "opus" || applied["applied_policy_version"] != "v12" || applied["proposed_at"] != "2026-09-28T15:00:00Z" {
		t.Errorf("applied item = %v", applied)
	}
	if changes := applied["proposed_changes"].(map[string]any); changes["policy.long.min_probability"] != 0.68 {
		t.Errorf("proposed_changes = %v, want key -> new value", changes)
	}
	if review := applied["review"].(map[string]any); review["verdict"] != "approve" || review["reason"] != "sound" {
		t.Errorf("review = %v", review)
	}
	bt := applied["backtest_result"].(map[string]any)
	if bt["expectancy_delta_pct"].(float64) < 19.99 || bt["expectancy_delta_pct"].(float64) > 20.01 || bt["max_drawdown_delta_pct"] != -20.0 {
		t.Errorf("backtest_result = %v, want +20%% Expectancy and -20%% Max Drawdown deltas", bt)
	}

	rejected := items[1].(map[string]any)
	if review := rejected["review"].(map[string]any); review["reason"] != "llm_output_out_of_bounds" {
		t.Errorf("rejected review = %v, want the llm_output_out_of_bounds reason", review)
	}
	if rejected["backtest_result"] != nil || rejected["applied_policy_version"] != nil {
		t.Errorf("rejected item = %v, want null backtest_result/applied_policy_version", rejected)
	}
}

func TestNew_APIPolicyProposalsReturnsAppliedAndRollbackAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	appliedAt := time.Date(2026, 9, 28, 16, 0, 0, 0, time.UTC)
	rolledBackAt := time.Date(2026, 10, 6, 16, 0, 0, 0, time.UTC)
	reason := "realized expectancy degraded from 1000.0000 to 700.0000 (>=20% relative)"
	rolledBack := domain.PolicyProposal{
		ID: 43, ProposedAt: time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC), ProposedBy: "sol", Status: domain.PolicyProposalStatusRolledBack,
		ProposedChangesJSON:  `[{"key":"policy.long.min_probability","old_value":"0.6","new_value":"0.68"}]`,
		AppliedPolicyVersion: strPtr("sol-43"), AppliedAt: &appliedAt, RolledBackAt: &rolledBackAt, RolledBackReason: &reason,
	}
	pending := domain.PolicyProposal{
		ID: 44, ProposedAt: time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC), ProposedBy: "sol", Status: domain.PolicyProposalStatusPending,
		ProposedChangesJSON: `[{"key":"policy.long.min_probability","old_value":"0.6","new_value":"0.62"}]`,
	}
	engine := router.New(router.WithPolicyProposalSource(proposals.StaticPolicyProposalSource{Proposals: []domain.PolicyProposal{rolledBack, pending}}))

	code, body := getProposals(t, engine, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, body)
	}
	items := body["items"].([]any)
	got := items[0].(map[string]any)
	if got["applied_at"] != "2026-09-28T16:00:00Z" || got["rolled_back_at"] != "2026-10-06T16:00:00Z" || got["rolled_back_reason"] != reason {
		t.Errorf("rolled_back item = %v, want the stored applied_at/rolled_back_at/rolled_back_reason", got)
	}

	never := items[1].(map[string]any)
	for _, key := range []string{"applied_at", "rolled_back_at", "rolled_back_reason"} {
		v, present := never[key]
		if !present || v != nil {
			t.Errorf("pending item %s = (%v, present=%v), want an explicit null", key, v, present)
		}
	}
}

func TestNew_APIPolicyProposalsFiltersByStatusAndLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New(router.WithPolicyProposalSource(proposals.StaticPolicyProposalSource{Proposals: proposalFixtures(t)}))

	_, body := getProposals(t, engine, "?status=rejected")
	items := body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != float64(41) {
		t.Errorf("status=rejected items = %v, want only proposal 41", items)
	}

	_, body = getProposals(t, engine, "?limit=1")
	if items := body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != float64(42) {
		t.Errorf("limit=1 items = %v, want only the newest proposal", items)
	}

	_, body = getProposals(t, engine, "?status=pending")
	if items := body["items"].([]any); len(items) != 0 {
		t.Errorf("status=pending items = %v, want none", items)
	}
}

func TestNew_APIPolicyProposalsRejectsInvalidQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	for _, query := range []string{"?status=bogus", "?limit=0", "?limit=201", "?limit=abc"} {
		if code, _ := getProposals(t, engine, query); code != http.StatusUnprocessableEntity {
			t.Errorf("GET policy-proposals%s status = %d, want 422", query, code)
		}
	}
	if code, _ := getProposals(t, engine, "?limit=200"); code != http.StatusOK {
		t.Errorf("limit=200 status = %d, want 200 (the maximum)", code)
	}
}

func TestNew_OpenAPIDescribesPolicyProposalsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}
	get, ok := spec.Paths["/policy-proposals"]["get"]
	if !ok {
		t.Fatalf("openapi.json has no GET policy-proposals path; got %d paths", len(spec.Paths))
	}
	for _, want := range []string{`"status"`, `"limit"`, `"rolled_back"`} {
		if !strings.Contains(string(get), want) {
			t.Errorf("GET /api/v1/policy-proposals operation lacks %s: %s", want, get)
		}
	}
}

func TestNew_APICalibrationReturnsMetricsFromWithCalibrationSourceOption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := calibration.StaticCalibrationSource{
		Metrics_: domain.CalibrationMetrics{
			Buckets: []domain.ConfidenceBucket{
				{Range: "0.50-0.60", AvgConfidence: 0.55, DirectionAccuracy: 0.51, AvgFutureReturnPct: -0.05, TradeCount: 3, TotalPnL: -1200, AvgPnLPct: -0.4},
			},
			ByDirection: []domain.DirectionMetric{
				{Direction: domain.JevDirectionLong, SampleCount: 10, DirectionAccuracy: 0.6, AvgFutureReturnPct: 0.12},
			},
			BrierScore:               0.19,
			LogLoss:                  0.52,
			ExpectedCalibrationError: 0.06,
		},
	}
	engine := router.New(router.WithCalibrationSource(source))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calibration", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"range":"0.50-0.60"`) || !strings.Contains(body, `"brier_score":0.19`) {
		t.Fatalf("expected body to contain the injected calibration metrics, got %q", body)
	}
	for _, want := range []string{
		`"avg_confidence":0.55`, `"trade_count":3`, `"total_pnl":-1200`, `"avg_pnl_pct":-0.4`,
		`"by_direction":[{"direction":"LONG","sample_count":10,"direction_accuracy":0.6,"avg_future_return_pct":0.12}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func TestNew_CalibrationPageRendersHeatmapIslandAndNavLinksBetweenScannerAndCalibration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()

	req := httptest.NewRequest(http.MethodGet, "/calibration", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("expected document shell, got %q", body)
	}
	for _, want := range []string{
		"<pitha-calibration-heatmap",
		`href="/scanner"`,
		`href="/calibration"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}
