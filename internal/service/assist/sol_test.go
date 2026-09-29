package assist_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

func newSol(t *testing.T, handler http.HandlerFunc) *assist.Sol {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return assist.NewSol(assist.NewClient(assist.Config{Label: "sol", BaseURL: server.URL, APIKey: "sol-key", MaxAttempts: 1}))
}

func solInput() assist.SolAnalysisInput {
	return assist.SolAnalysisInput{
		Long: assist.DirectionCalibration{
			Thresholds: config.PolicyDirectionThresholds{MinProbability: 0.60, MinEntryQuality: domain.JevEntryQualityGood},
			Calibration: domain.CalibrationMetrics{
				Buckets:     []domain.ConfidenceBucket{{Range: "0.60-0.70", DirectionAccuracy: 0.4, AvgFutureReturnPct: -0.5, SampleCount: 40}},
				SampleCount: 40,
			},
		},
	}
}

func TestSol_Analyze_SendsCalibrationAndConstraintsAndReturnsLLMProposal(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]json.RawMessage
	sol := newSol(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"rationale":{"why":"weak bucket"},"proposed_changes":[{"key":"policy.long.min_probability","new_value":0.65}]}`))
	})

	proposal, ok, err := sol.Analyze(context.Background(), solInput())
	if err != nil || !ok {
		t.Fatalf("Analyze = (%+v, %v, %v), want a proposal", proposal, ok, err)
	}
	if gotAuth != "Bearer sol-key" || gotPath != assist.SolAnalyzePath {
		t.Errorf("request auth/path = %q/%q", gotAuth, gotPath)
	}
	var constraints struct {
		AllowedKeys       []string `json:"allowed_keys"`
		MaxConfidenceStep float64  `json:"max_confidence_step"`
	}
	if err := json.Unmarshal(gotBody["constraints"], &constraints); err != nil {
		t.Fatalf("decode constraints: %v", err)
	}
	if len(constraints.AllowedKeys) != len(domain.PolicyProposalKeys) || constraints.MaxConfidenceStep != 0.05 {
		t.Errorf("constraints = %+v, want every policy.* key and the 0.05 cap", constraints)
	}
	if string(gotBody["long"]) == "" || !json.Valid(gotBody["long"]) {
		t.Error("request has no long-direction calibration")
	}
	if len(proposal.Changes) != 1 || proposal.Changes[0].Key != domain.PolicyKeyLongMinProbability || string(proposal.Changes[0].NewValue) != "0.65" {
		t.Errorf("Changes = %+v", proposal.Changes)
	}
	if proposal.RationaleJSON != `{"why":"weak bucket"}` {
		t.Errorf("RationaleJSON = %q, want the LLM's rationale verbatim", proposal.RationaleJSON)
	}
}

func TestSol_Analyze_NoChangesMeansNoProposal(t *testing.T) {
	sol := newSol(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"proposed_changes":[]}`)) })
	if _, ok, err := sol.Analyze(context.Background(), solInput()); err != nil || ok {
		t.Errorf("Analyze = (_, %v, %v), want (false, nil)", ok, err)
	}
}

func TestSol_Analyze_MissingRationaleIsStoredAsEmptyObject(t *testing.T) {
	sol := newSol(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"proposed_changes":[{"key":"policy.long.min_probability","new_value":0.65}]}`))
	})
	proposal, _, err := sol.Analyze(context.Background(), solInput())
	if err != nil || proposal.RationaleJSON != "{}" {
		t.Errorf("Analyze = (%+v, %v), want RationaleJSON {}", proposal, err)
	}
}

func TestSol_Analyze_APIFailureIsAnError(t *testing.T) {
	sol := newSol(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) })
	if _, _, err := sol.Analyze(context.Background(), solInput()); err == nil {
		t.Error("Analyze succeeded despite a 503")
	}
}

func TestSol_Analyze_UnconfiguredReturnsErrNotConfigured(t *testing.T) {
	sol := assist.NewSol(assist.NewClient(assist.Config{Label: "sol", RetryBaseDelay: time.Millisecond}))
	if _, _, err := sol.Analyze(context.Background(), solInput()); !errors.Is(err, assist.ErrNotConfigured) {
		t.Errorf("Analyze error = %v, want ErrNotConfigured", err)
	}
}
