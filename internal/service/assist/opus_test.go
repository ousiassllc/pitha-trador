package assist_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

// opusFixture is a fake Opus API answering answer (a raw JSON body) and
// counting calls.
func opusFixture(t *testing.T, answer string) (*assist.Opus, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var calls atomic.Int32
	var lastBody atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var raw json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		lastBody.Store([]byte(raw))
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(server.Close)
	return assist.NewOpus(assist.NewClient(assist.Config{Label: "opus", BaseURL: server.URL, APIKey: "opus-key", MaxAttempts: 1})), &calls, &lastBody
}

func reviewInput(baseExp, candExp, baseDD, candDD float64) assist.OpusReviewInput {
	return assist.OpusReviewInput{
		RationaleJSON: `{"why":"x"}`,
		Changes:       []domain.PolicyChange{{Key: domain.PolicyKeyLongMinProbability, OldValue: "0.6", NewValue: "0.65"}},
		Comparison: assist.BacktestComparison{
			BaselineExpectancy: baseExp, CandidateExpectancy: candExp,
			BaselineMaxDrawdownPct: baseDD, CandidateMaxDrawdownPct: candDD,
		},
	}
}

func decodeReview(t *testing.T, reviewJSON string) assist.OpusReview {
	t.Helper()
	var review assist.OpusReview
	if err := json.Unmarshal([]byte(reviewJSON), &review); err != nil {
		t.Fatalf("unmarshal review json: %v", err)
	}
	return review
}

func TestOpus_Review_ApprovesOnlyWhenThresholdsMetAndAPIApproves(t *testing.T) {
	opus, calls, body := opusFixture(t, `{"verdict":"approve","reason":"looks sane"}`)

	approved, reviewJSON, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5))
	if err != nil || !approved {
		t.Fatalf("Review = (%v, %s, %v), want approved", approved, reviewJSON, err)
	}
	review := decodeReview(t, reviewJSON)
	if review.Verdict != "approve" || !review.Approved || !review.DeterministicPassed || !review.LLMReviewed || review.Reason != "looks sane" {
		t.Errorf("review = %+v", review)
	}
	if calls.Load() != 1 {
		t.Fatalf("Opus API calls = %d, want 1", calls.Load())
	}
	var sent struct {
		Deterministic struct {
			Passed bool `json:"passed"`
		} `json:"deterministic"`
		Proposal struct {
			Changes []struct {
				Key string `json:"key"`
			} `json:"changes"`
		} `json:"proposal"`
	}
	sentBody, _ := body.Load().([]byte)
	if err := json.Unmarshal(sentBody, &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !sent.Deterministic.Passed || len(sent.Proposal.Changes) != 1 || sent.Proposal.Changes[0].Key != domain.PolicyKeyLongMinProbability {
		t.Errorf("request = %s, want the proposal plus the deterministic verdict", sentBody)
	}
}

func TestOpus_Review_APIRejectVetoesAProposalThatMeetsThresholds(t *testing.T) {
	opus, _, _ := opusFixture(t, `{"verdict":"reject","reason":"overfits recent regime"}`)

	approved, reviewJSON, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5))
	if err != nil || approved {
		t.Fatalf("Review = (%v, %v), want a rejection", approved, err)
	}
	review := decodeReview(t, reviewJSON)
	if review.Verdict != "reject" || review.Approved || !review.DeterministicPassed || review.Reason != "overfits recent regime" {
		t.Errorf("review = %+v", review)
	}
}

func TestOpus_Review_APIApproveCannotOverrideMissedThresholds(t *testing.T) {
	tests := map[string]assist.OpusReviewInput{
		"expectancy worsens":     reviewInput(0.20, 0.15, 5, 5),
		"drawdown beyond 10%":    reviewInput(0.20, 0.25, 10, 11.5),
		"baseline had no budget": reviewInput(0.20, 0.25, 0, 0.1),
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			opus, calls, _ := opusFixture(t, `{"verdict":"approve","reason":"fine"}`)
			approved, reviewJSON, err := opus.Review(context.Background(), in)
			if err != nil || approved {
				t.Fatalf("Review = (%v, %v), want a deterministic rejection", approved, err)
			}
			review := decodeReview(t, reviewJSON)
			if review.Verdict != "reject" || review.DeterministicPassed || review.LLMReviewed {
				t.Errorf("review = %+v, want reject without consulting the API", review)
			}
			if calls.Load() != 0 {
				t.Errorf("Opus API was called %d time(s) for a proposal that already missed the thresholds", calls.Load())
			}
		})
	}
}

func TestOpus_Review_ExactlyTenPercentDrawdownStillReachesTheAPI(t *testing.T) {
	opus, calls, _ := opusFixture(t, `{"verdict":"approve","reason":"ok"}`)
	approved, _, err := opus.Review(context.Background(), reviewInput(0.20, 0.20, 10, 11)) // exactly +10% relative
	if err != nil || !approved || calls.Load() != 1 {
		t.Fatalf("Review = (%v, %v) after %d call(s), want approved (boundary is inclusive)", approved, err, calls.Load())
	}
}

func TestOpus_Review_APIFailureOrBadVerdictIsAnErrorNotADecision(t *testing.T) {
	opus, _, _ := opusFixture(t, `{"verdict":"maybe","reason":"?"}`)
	if approved, _, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5)); err == nil || approved {
		t.Errorf("Review with an unrecognized verdict = (%v, %v), want an error", approved, err)
	}

	unconfigured := assist.NewOpus(assist.NewClient(assist.Config{Label: "opus"}))
	if _, _, err := unconfigured.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5)); !errors.Is(err, assist.ErrNotConfigured) {
		t.Errorf("unconfigured Review error = %v, want ErrNotConfigured", err)
	}
}
