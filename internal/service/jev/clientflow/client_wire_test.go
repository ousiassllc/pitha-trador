package clientflow_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// capturedRequest is what the official API receives.
type capturedRequest struct {
	Method, Path, Auth, ContentType string
	Body                            struct {
		State     map[string]json.RawMessage `json:"state"`
		Model     string                     `json:"model"`
		Questions map[string]struct {
			Type         string                     `json:"type"`
			Instructions string                     `json:"instructions"`
			Criteria     map[string]json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
}

// captureServer records the one request it receives and replies with
// the verbatim official-API response body.
func captureServer(t *testing.T, responseBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Method, got.Path = r.Method, r.URL.Path
		got.Auth, got.ContentType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got.Body); err != nil {
			t.Errorf("request body is not valid JSON: %v", err)
		}
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server, got
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

const scoutWireResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "interesting_now":   {"type": "noul", "noul": 0.81},
    "momentum_quality":  {"type": "choice", "choice": "strong", "probabilities": {"weak": 0.02, "moderate": 0.18, "strong": 0.7, "exceptional": 0.1}, "confidence": 0.7},
    "liquidity_ok":      {"type": "noul", "noul": 0.92},
    "abnormal_activity": {"type": "noul", "noul": 0.33}
  },
  "usage": {"input_tokens": 1534, "output_tokens": 62}
}`

const traderWireResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "direction":                {"type": "choice", "choice": "SHORT", "probabilities": {"LONG": 0.1, "SHORT": 0.66, "NONE": 0.24}, "confidence": 0.66},
    "regime":                   {"type": "choice", "choice": "BREAKOUT", "probabilities": {"TREND": 0.2, "RANGE": 0.05, "BREAKOUT": 0.7, "CHAOTIC": 0.05}, "confidence": 0.7},
    "entry_quality":            {"type": "choice", "choice": "good", "probabilities": {"poor": 0.05, "fair": 0.2, "good": 0.5, "strong": 0.2, "exceptional": 0.05}, "confidence": 0.5},
    "toxic_flow":               {"type": "noul", "noul": 0.12},
    "liquidity_stressed":       {"type": "noul", "noul": 0.07},
    "continuation_probability": {"type": "noul", "noul": 0.58}
  },
  "usage": {"input_tokens": 1534, "output_tokens": 71}
}`

func TestClient_Scout_SendsOfficialSystemOneRequest(t *testing.T) {
	server, got := captureServer(t, scoutWireResponse)
	client := jev.NewClient(jev.Config{BaseURL: server.URL, APIKey: "sk-test"})

	_, _, err := client.Scout(context.Background(), jev.ScoutRequest{
		QuestionVersion: jev.ScoutQuestionVersion,
		State:           jev.ScoutState{Symbol: "7203"},
		RAGContext:      rag.Context{Cases: []rag.SimilarCase{{Source: "jev_decision"}}},
	})
	if err != nil {
		t.Fatalf("Scout: %v", err)
	}

	if got.Method != http.MethodPost || got.Path != "/v1/systemone" {
		t.Errorf("request = %s %s, want POST /v1/systemone", got.Method, got.Path)
	}
	if got.Auth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", got.Auth, "Bearer sk-test")
	}
	if !strings.HasPrefix(got.ContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got.ContentType)
	}
	if got.Body.Model != "jev-latest" {
		t.Errorf("model = %q, want jev-latest", got.Body.Model)
	}
	if want := []string{"market", "similar_past_cases"}; !reflect.DeepEqual(keys(got.Body.State), want) {
		t.Errorf("state keys = %v, want %v", keys(got.Body.State), want)
	}
	var market struct {
		Symbol string `json:"symbol"`
	}
	if err := json.Unmarshal(got.Body.State["market"], &market); err != nil || market.Symbol != "7203" {
		t.Errorf("state.market = %s, want the ScoutState (symbol 7203)", got.Body.State["market"])
	}
	if !strings.Contains(string(got.Body.State["similar_past_cases"]), `"cases"`) {
		t.Errorf("state.similar_past_cases = %s, want the RAG context", got.Body.State["similar_past_cases"])
	}

	wantTypes := map[string]string{
		"interesting_now": "noul", "momentum_quality": "choice", "liquidity_ok": "noul", "abnormal_activity": "noul",
	}
	assertQuestions(t, got, wantTypes, map[string][]string{
		"momentum_quality": {"weak", "moderate", "strong", "exceptional"},
	})
}

func TestClient_Trader_SendsOfficialSystemOneRequest(t *testing.T) {
	server, got := captureServer(t, traderWireResponse)
	client := jev.NewClient(jev.Config{BaseURL: server.URL, APIKey: "sk-test"})

	if _, _, err := client.Trader(context.Background(), jev.TraderRequest{State: jev.ScoutState{Symbol: "7203"}}); err != nil {
		t.Fatalf("Trader: %v", err)
	}

	if got.Method != http.MethodPost || got.Path != "/v1/systemone" || got.Body.Model != "jev-latest" {
		t.Errorf("request = %s %s model=%q, want POST /v1/systemone jev-latest", got.Method, got.Path, got.Body.Model)
	}
	if want := []string{"market", "similar_past_cases"}; !reflect.DeepEqual(keys(got.Body.State), want) {
		t.Errorf("state keys = %v, want %v", keys(got.Body.State), want)
	}
	assertQuestions(t, got, map[string]string{
		"direction": "choice", "regime": "choice", "entry_quality": "choice",
		"toxic_flow": "noul", "liquidity_stressed": "noul", "continuation_probability": "noul",
	}, map[string][]string{
		"direction":     {"LONG", "SHORT", "NONE"},
		"regime":        {"TREND", "RANGE", "BREAKOUT", "CHAOTIC"},
		"entry_quality": {"poor", "fair", "good", "strong", "exceptional"},
	})
}

// assertQuestions checks the question set against the official schema:
// exact ids/types, non-empty instructions, noul criteria {true,false},
// choice criteria option -> non-empty rubric.
func assertQuestions(t *testing.T, got *capturedRequest, wantTypes map[string]string, wantOptions map[string][]string) {
	t.Helper()
	if !reflect.DeepEqual(keys(got.Body.Questions), keys(wantTypes)) {
		t.Fatalf("question ids = %v, want %v", keys(got.Body.Questions), keys(wantTypes))
	}
	for id, q := range got.Body.Questions {
		if q.Type != wantTypes[id] {
			t.Errorf("question %q type = %q, want %q", id, q.Type, wantTypes[id])
		}
		if strings.TrimSpace(q.Instructions) == "" {
			t.Errorf("question %q has no instructions", id)
		}
		switch q.Type {
		case "noul":
			if want := []string{"false", "true"}; !reflect.DeepEqual(keys(q.Criteria), want) {
				t.Errorf("question %q criteria keys = %v, want %v", id, keys(q.Criteria), want)
			}
		case "choice":
			want := slices.Clone(wantOptions[id])
			slices.Sort(want)
			if !reflect.DeepEqual(keys(q.Criteria), want) {
				t.Errorf("question %q options = %v, want %v", id, keys(q.Criteria), want)
			}
		}
		for k, v := range q.Criteria {
			if string(v) == `""` {
				t.Errorf("question %q criteria %q is empty", id, k)
			}
		}
	}
}

func TestClient_Scout_MapsOfficialResponse(t *testing.T) {
	server, _ := captureServer(t, scoutWireResponse)
	client := jev.NewClient(jev.Config{BaseURL: server.URL})

	resp, _, err := client.Scout(context.Background(), jev.ScoutRequest{})
	if err != nil {
		t.Fatalf("Scout: %v", err)
	}
	want := jev.ScoutResponse{
		InterestingNow: 0.81, MomentumQuality: jev.MomentumQualityStrong, LiquidityOk: 0.92, AbnormalActivity: 0.33,
		ModelID: "jev-1.13.0",
	}
	if resp != want {
		t.Errorf("Scout() = %+v, want %+v (request_cost must stay nil: the API reports tokens only)", resp, want)
	}
}

func TestClient_Trader_MapsOfficialResponseWithDirectionConfidence(t *testing.T) {
	server, _ := captureServer(t, traderWireResponse)
	client := jev.NewClient(jev.Config{BaseURL: server.URL})

	resp, _, err := client.Trader(context.Background(), jev.TraderRequest{})
	if err != nil {
		t.Fatalf("Trader: %v", err)
	}
	want := jev.TraderResponse{
		Direction: "SHORT", Regime: "BREAKOUT", EntryQuality: "good",
		ToxicFlow: 0.12, LiquidityStressed: 0.07, ContinuationProbability: 0.58,
		Confidence: 0.66, // the direction answer's confidence, not regime's (0.7)
		ModelID:    "jev-1.13.0",
	}
	if resp != want {
		t.Errorf("Trader() = %+v, want %+v", resp, want)
	}
}

// BaseURL is the host (the Settings screen asks for it without a path):
// a trailing slash must not produce "//v1/systemone", and a path prefix
// is kept in front of the endpoint.
func TestClient_Scout_NormalizesBaseURLTrailingSlash(t *testing.T) {
	for name, suffix := range map[string]string{"trailing slash": "/", "path prefix": "/api", "path prefix with slash": "/api/"} {
		t.Run(name, func(t *testing.T) {
			server, got := captureServer(t, scoutWireResponse)
			client := jev.NewClient(jev.Config{BaseURL: server.URL + suffix})
			if _, _, err := client.Scout(context.Background(), jev.ScoutRequest{}); err != nil {
				t.Fatalf("Scout: %v", err)
			}
			want := strings.TrimRight(suffix, "/") + "/v1/systemone"
			if got.Path != want {
				t.Errorf("request path = %q, want %q", got.Path, want)
			}
		})
	}
}
