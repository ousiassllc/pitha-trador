package trialwire_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// trialResponse is an OpenRouter-style response (issue #1058): one refusal, a
// choice without confidence, and usage.cost, id and provider.
const trialResponse = `{
  "id": "gen-dec-1789738314-X5e5eKGQdvR9rblyX250",
  "model": "cloudflare/clef-flash-20261001",
  "provider": "DeepInfra",
  "answers": {
    "direction":                {"type": "choice", "choice": "LONG"},
    "regime":                   {"type": "choice", "choice": "TREND", "probabilities": {"TREND": 0.8, "RANGE": 0.2}, "confidence": 0.8},
    "entry_quality":            {"type": "refusal"},
    "toxic_flow":               {"type": "noul", "noul": 0.1},
    "liquidity_stressed":       {"type": "noul", "noul": 0.2},
    "continuation_probability": {"type": "noul", "noul": 0.6}
  },
  "usage": {"input_tokens": 200, "output_tokens": 5, "cost": 0.0000076}
}`

// liveResponse is a complete strict answer set (confidence on every choice).
const liveResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "direction":                {"type": "choice", "choice": "SHORT", "probabilities": {"LONG": 0.1, "SHORT": 0.66, "NONE": 0.24}, "confidence": 0.66},
    "regime":                   {"type": "choice", "choice": "BREAKOUT", "confidence": 0.7},
    "entry_quality":            {"type": "choice", "choice": "good", "confidence": 0.5},
    "toxic_flow":               {"type": "noul", "noul": 0.12},
    "liquidity_stressed":       {"type": "noul", "noul": 0.07},
    "continuation_probability": {"type": "noul", "noul": 0.58}
  }
}`

type capture struct {
	calls  atomic.Int32
	path   string
	auth   string
	status int
	reply  string
	body   map[string]json.RawMessage
}

func newServer(t *testing.T, status int, reply string) (*httptest.Server, *capture) {
	t.Helper()
	got := &capture{status: status, reply: reply}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.calls.Add(1)
		got.path, got.auth = r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		got.body = nil
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(got.status)
		_, _ = w.Write([]byte(got.reply))
	}))
	t.Cleanup(server.Close)
	return server, got
}

func traderRequest() jev.TraderRequest {
	return jev.TraderRequest{State: jev.ScoutState{Symbol: "7203"}, RAGContext: rag.Context{}}
}

func TestTrader_ReadsRefusalOptionalFieldsCostProviderAndID(t *testing.T) {
	server, _ := newServer(t, http.StatusOK, trialResponse)
	client := trialwire.New(trialwire.Config{BaseURL: server.URL, APIKey: "sk-test", Model: "cloudflare/clef-flash"})

	resp, _, bytes, err := client.Trader(context.Background(), traderRequest())
	if err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if resp.Direction != "LONG" || resp.Confidence != nil {
		t.Errorf("direction = %q confidence = %v, want LONG with no confidence", resp.Direction, resp.Confidence)
	}
	if resp.Regime != "TREND" || resp.EntryQuality != "" || len(resp.Refused) != 1 || resp.Refused[0] != "entry_quality" {
		t.Errorf("regime = %q entry_quality = %q refused = %v, want TREND, \"\" and [entry_quality]", resp.Regime, resp.EntryQuality, resp.Refused)
	}
	if resp.ToxicFlow == nil || *resp.ToxicFlow != 0.1 || resp.ContinuationProbability == nil || *resp.ContinuationProbability != 0.6 {
		t.Errorf("noul answers = %v/%v, want 0.1 and 0.6", resp.ToxicFlow, resp.ContinuationProbability)
	}
	if resp.ReportedCostUSD == nil || *resp.ReportedCostUSD != 0.0000076 || resp.Provider != "DeepInfra" || resp.ResponseID != "gen-dec-1789738314-X5e5eKGQdvR9rblyX250" {
		t.Errorf("cost/provider/id = %v/%q/%q", resp.ReportedCostUSD, resp.Provider, resp.ResponseID)
	}
	if resp.InputTokens != 200 || resp.OutputTokens != 5 || bytes == 0 || resp.Body != trialResponse {
		t.Errorf("usage = %d/%d, request bytes = %d, body kept = %v", resp.InputTokens, resp.OutputTokens, bytes, resp.Body == trialResponse)
	}
}

func TestTrader_AnAbsentCostIsNilNotZero(t *testing.T) {
	server, _ := newServer(t, http.StatusOK, liveResponse)
	resp, _, _, err := trialwire.New(trialwire.Config{BaseURL: server.URL}).Trader(context.Background(), traderRequest())
	if err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if resp.ReportedCostUSD != nil || resp.InputTokens != 0 {
		t.Errorf("cost/tokens = %v/%d, want nil/0 for a response without usage", resp.ReportedCostUSD, resp.InputTokens)
	}
	if resp.Confidence == nil || *resp.Confidence != 0.66 || resp.DirectionProbabilities["SHORT"] != 0.66 {
		t.Errorf("confidence/probabilities = %v/%v, want the reported 0.66", resp.Confidence, resp.DirectionProbabilities)
	}
}

// The default endpoint is the live System One path, the default base URL is not
// the live host, and the request carries no routing preference unless asked.
func TestClient_DefaultsAndNoRoutingFieldsByDefault(t *testing.T) {
	if trialwire.DefaultEndpoint != jev.Endpoint {
		t.Errorf("DefaultEndpoint = %q, want the live %q", trialwire.DefaultEndpoint, jev.Endpoint)
	}
	if trialwire.DefaultBaseURL == jev.DefaultBaseURL {
		t.Errorf("DefaultBaseURL = the live host %q", jev.DefaultBaseURL)
	}
	server, got := newServer(t, http.StatusOK, trialResponse)
	if _, _, _, err := trialwire.New(trialwire.Config{BaseURL: server.URL + "/", APIKey: "sk-test", Model: "m"}).Trader(context.Background(), traderRequest()); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if got.path != "/v1/systemone" || got.auth != "Bearer sk-test" {
		t.Errorf("path/auth = %q/%q, want /v1/systemone with the bearer key", got.path, got.auth)
	}
	if _, ok := got.body["provider"]; ok {
		t.Errorf("a request without Config.Provider carries provider: %s", got.body["provider"])
	}
}

func TestClient_SwitchesEndpointAndSendsProviderPreferences(t *testing.T) {
	server, got := newServer(t, http.StatusOK, trialResponse)
	noFallbacks, zdr := false, true
	client := trialwire.New(trialwire.Config{
		BaseURL: server.URL, APIKey: "sk-test", Model: "cloudflare/clef-flash", Endpoint: trialwire.DecisionsEndpoint,
		Provider: &systemone.Provider{Only: []string{"deepinfra"}, AllowFallbacks: &noFallbacks, DataCollection: "deny", ZDR: &zdr},
	})
	if _, _, _, err := client.Trader(context.Background(), traderRequest()); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	if got.path != "/alpha/decisions" {
		t.Errorf("path = %q, want /alpha/decisions", got.path)
	}
	want := `{"only":["deepinfra"],"allow_fallbacks":false,"data_collection":"deny","zdr":true}`
	if string(got.body["provider"]) != want {
		t.Errorf("provider = %s, want %s", got.body["provider"], want)
	}
	if string(got.body["model"]) != `"cloudflare/clef-flash"` {
		t.Errorf("model = %s", got.body["model"])
	}
}

// The trial asks exactly what the live Trader asks: the same state and the same
// questions, byte for byte (only the model and the routing differ).
func TestTrader_SendsTheSameStateAndQuestionsAsTheLiveTrader(t *testing.T) {
	req := traderRequest()
	req.State.Price = 1234.5

	liveServer, live := newServer(t, http.StatusOK, liveResponse)
	if _, _, err := jev.NewClient(jev.Config{BaseURL: liveServer.URL}).Trader(context.Background(), req); err != nil {
		t.Fatalf("live Trader: %v", err)
	}
	trialServer, trial := newServer(t, http.StatusOK, liveResponse)
	if _, _, _, err := trialwire.New(trialwire.Config{BaseURL: trialServer.URL}).Trader(context.Background(), req); err != nil {
		t.Fatalf("trial Trader: %v", err)
	}
	for _, field := range []string{"state", "questions"} {
		if string(live.body[field]) != string(trial.body[field]) {
			t.Errorf("%s differs between live and trial:\nlive  %s\ntrial %s", field, live.body[field], trial.body[field])
		}
	}
}

// Every registered trader version asks exactly the question ids the trial reads.
func TestTrader_ReadsTheQuestionIDsEveryVersionAsks(t *testing.T) {
	want := []string{"continuation_probability", "direction", "entry_quality", "liquidity_stressed", "regime", "toxic_flow"}
	for _, version := range jev.TraderVersions() {
		questions, err := jev.TraderQuestionsFor(version)
		if err != nil {
			t.Fatalf("TraderQuestionsFor(%q): %v", version, err)
		}
		var got []string
		for id := range questions {
			got = append(got, id)
		}
		if len(got) != len(want) {
			t.Errorf("%s asks %v, want %v", version, got, want)
		}
		for _, id := range want {
			if _, ok := questions[id]; !ok {
				t.Errorf("%s does not ask %q", version, id)
			}
		}
	}
}

func TestTrader_MakesOneAttemptAndKeepsTheStatus(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529, http.StatusPaymentRequired, http.StatusForbidden, http.StatusBadGateway, http.StatusRequestEntityTooLarge} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server, got := newServer(t, status, `{"error":{"code":1,"message":"nope"}}`)
			_, _, requestBytes, err := trialwire.New(trialwire.Config{BaseURL: server.URL}).Trader(context.Background(), traderRequest())
			var apiErr *jev.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("Trader error = %v, want *jev.APIError %d", err, status)
			}
			if got.calls.Load() != 1 {
				t.Errorf("calls = %d, want 1 (a paid trial call is not retried)", got.calls.Load())
			}
			if requestBytes == 0 {
				t.Errorf("request bytes = 0 on a failed call, want the size of the request that was sent")
			}
		})
	}
}

func TestTrader_AnInvalidAnswerSetFails(t *testing.T) {
	server, _ := newServer(t, http.StatusOK, `{"model":"m","answers":{}}`)
	_, _, _, err := trialwire.New(trialwire.Config{BaseURL: server.URL}).Trader(context.Background(), traderRequest())
	if !errors.Is(err, systemone.ErrInvalidResponse) {
		t.Errorf("Trader error = %v, want ErrInvalidResponse", err)
	}
}

func TestTrader_UnregisteredVersionSendsNothing(t *testing.T) {
	server, got := newServer(t, http.StatusOK, trialResponse)
	req := traderRequest()
	req.QuestionVersion = "no-such-version"
	_, _, _, err := trialwire.New(trialwire.Config{BaseURL: server.URL}).Trader(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "no-such-version") {
		t.Errorf("Trader error = %v, want an unregistered-version error", err)
	}
	if got.calls.Load() != 0 {
		t.Errorf("calls = %d, want 0", got.calls.Load())
	}
}

func TestClient_ConfiguredNeedsAKey(t *testing.T) {
	if trialwire.New(trialwire.Config{}).Configured() || !trialwire.New(trialwire.Config{APIKey: "k"}).Configured() {
		t.Error("Configured must be true exactly when a key is set")
	}
}

// The live validation is not loosened by the trial (issue #1058): the very
// response the trial reads is an invalid one for the live Trader, and is not retried.
func TestLiveTrader_StillRejectsTheTrialResponse(t *testing.T) {
	server, got := newServer(t, http.StatusOK, trialResponse)
	_, _, err := jev.NewClient(jev.Config{BaseURL: server.URL}).Trader(context.Background(), traderRequest())
	if !errors.Is(err, systemone.ErrInvalidResponse) {
		t.Fatalf("live Trader error = %v, want ErrInvalidResponse", err)
	}
	if got.calls.Load() != 1 {
		t.Errorf("live calls = %d, want 1", got.calls.Load())
	}
	if _, ok := got.body["provider"]; ok {
		t.Errorf("the live call carries provider")
	}
}
