package trialflow_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// chatArm is a chat arm on the application's real wiring, priced below the chat
// worst case (so the counting rule shows). Its arm cap is raised: a chat call is
// reserved at about 0.1 USD (the 2-bytes-per-token input estimate at the chat worst
// case plus the output limit), more than the default arm cap of 0.05.
func chatArm(id string) jevtrialsettings.Arm {
	return jevtrialsettings.Arm{ID: id, Provider: "openrouter", Model: "openai/gpt-6.2-astra", PromptVersion: "trader-v3",
		Endpoint: jevtrialsettings.EndpointChatCompletions, ListPriceUSDPerMtok: 2, ListPriceOutputUSDPerMtok: 10, DailyCostCapUSD: 1}
}

// raiseOverallCap lifts the overall daily cap (default 0.10) above a chat call's reservation.
func raiseOverallCap(t *testing.T, settings *system.RuntimeSettingsRepository) {
	t.Helper()
	if err := settings.Set(context.Background(), jevtrialsettings.KeyDailyCostCapUSD, "1", time.Now()); err != nil {
		t.Fatal(err)
	}
}

const chatReply = `{"id":"gen-1","model":"openai/gpt-6.2-astra-20261101","provider":"OpenAI","choices":[{"finish_reason":"stop","message":{"role":"assistant",` +
	`"content":"{\"direction\":{\"choice\":\"NONE\",\"confidence\":0.6},\"regime\":{\"choice\":\"RANGE\",\"confidence\":0.5},` +
	`\"entry_quality\":{\"choice\":\"poor\",\"confidence\":0.4},\"toxic_flow\":0.2,\"liquidity_stressed\":0.1,\"continuation_probability\":null}"}}],` +
	`"usage":{"prompt_tokens":1500,"completion_tokens":90,"cost":0.0123}}`

// chatRun runs one live decision against a chat arm whose OpenRouter stand-in answers
// reply with status, and returns the stored row and what was sent.
func chatRun(t *testing.T, status int, reply string) (jevtrial.Decision, string) {
	t.Helper()
	api := &apiRecorder{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	})}
	tr, settings, rows, instID, results := appTrader(t, trial.Keys{APIKey: "sk-test"}, api)
	enableApp(t, settings, chatArm("chat"))
	raiseOverallCap(t, settings)
	if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	waitFor(t, "the arm's row", func() bool { return rows() == 1 })
	d, err := results.Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if api.requests() != 1 {
		t.Fatalf("requests = %d, want 1 (a paid call is never retried)", api.requests())
	}
	return d, api.bodies[0]
}

// A chat arm posts a chat body, stores a structured answer as direction/entry_quality
// etc. with the reported cost, and a null answer stays NULL.
func TestChatArm_StoresTheStructuredAnswer(t *testing.T) {
	d, sent := chatRun(t, http.StatusOK, chatReply)
	for _, want := range []string{`"messages":[`, `"response_format":{"type":"json_schema"`, `"max_tokens":1024`, `"temperature":0`, `"data_collection":"deny"`} {
		if !strings.Contains(sent, want) {
			t.Errorf("the request lacks %s", want)
		}
	}
	if strings.Contains(sent, "require_parameters") {
		t.Error("require_parameters was sent: a model that does not list temperature would have no endpoint")
	}
	if strings.Contains(sent, `"state":`) && strings.Contains(sent, `"questions":`) {
		t.Error("System One's {state, questions} was sent to the chat endpoint")
	}
	if d.Status != jevtrial.StatusPartial || d.Direction == nil || *d.Direction != "NONE" || d.EntryQuality == nil || *d.EntryQuality != "poor" || d.ContinuationProbability != nil {
		t.Errorf("row = status %q direction %v entry_quality %v continuation %v, want partial, NONE, poor and NULL", d.Status, d.Direction, d.EntryQuality, d.ContinuationProbability)
	}
	if d.CostSource != jevtrial.CostReported || d.CostUSD == nil || *d.CostUSD != 0.0123 || d.CountedCostUSD != 0.0123 {
		t.Errorf("cost = %q %v counted %v, want reported 0.0123", d.CostSource, d.CostUSD, d.CountedCostUSD)
	}
	if d.ServedModel != "openai/gpt-6.2-astra-20261101" || d.ServedProvider != "OpenAI" || d.ResponseID != "gen-1" || d.RequestedModel != "openai/gpt-6.2-astra" {
		t.Errorf("model/provider/id = %q/%q/%q requested %q", d.ServedModel, d.ServedProvider, d.ResponseID, d.RequestedModel)
	}
}

// A message that is not a valid answer set is one failed row, invalid_response, counted at
// the call's reservation (the call may have been billed).
func TestChatArm_UnparsableAnswerIsAnInvalidResponseRowCountedAtTheReservation(t *testing.T) {
	d, sent := chatRun(t, http.StatusOK, `{"model":"m","choices":[{"message":{"role":"assistant","content":"Probably a long."}}]}`)
	if d.Status != jevtrial.StatusFailed || d.FailureCode != jevtrial.FailureInvalidResponse || d.CostSource != jevtrial.CostUnknown || d.CostUSD != nil {
		t.Fatalf("row = %q %q cost %q %v", d.Status, d.FailureCode, d.CostSource, d.CostUSD)
	}
	arm := chatArm("chat")
	want := trial.ReservedUSD(arm, len(sent))
	if d.CountedCostUSD != want || d.CountedCostUSD < float64(trialwire.ChatMaxOutputTokens)*jevtrialsettings.ChatWorstOutputUSDPerMtok/1e6 {
		t.Errorf("counted = %v, want the reservation %v (input plus the output limit)", d.CountedCostUSD, want)
	}
}

// A 4xx is refused before the model runs: one failed row, counted 0, with the status.
func TestChatArm_ClientErrorIsAHTTP4xxRowCountedZero(t *testing.T) {
	d, _ := chatRun(t, http.StatusBadRequest, `{"error":{"code":400,"message":"response_format is not supported by this model"}}`)
	if d.Status != jevtrial.StatusFailed || d.FailureCode != jevtrial.FailureHTTP4xx || d.HTTPStatus == nil || *d.HTTPStatus != 400 || d.CountedCostUSD != 0 || d.CostUSD != nil {
		t.Errorf("row = %q %q status %v counted %v cost %v", d.Status, d.FailureCode, d.HTTPStatus, d.CountedCostUSD, d.CostUSD)
	}
	if !strings.Contains(d.ErrorExcerpt, "response_format") {
		t.Errorf("error_excerpt = %q", d.ErrorExcerpt)
	}
}

// The chat endpoint constant of the settings is the wire layer's.
func TestChatEndpointConstantsMatchTheWireLayer(t *testing.T) {
	if jevtrialsettings.EndpointChatCompletions != trialwire.ChatCompletionsEndpoint {
		t.Errorf("EndpointChatCompletions = %q, trialwire.ChatCompletionsEndpoint = %q", jevtrialsettings.EndpointChatCompletions, trialwire.ChatCompletionsEndpoint)
	}
	if trialwire.ChatCompletionsEndpoint != "/v1/chat/completions" || strings.HasPrefix(trialwire.ChatCompletionsEndpoint, "/api/") {
		t.Errorf("ChatCompletionsEndpoint = %q, want /v1/chat/completions (the base URL already ends in /api)", trialwire.ChatCompletionsEndpoint)
	}
}

// The reservation of a chat call is sized by the chat body and holds its output
// limit: a cap that a stage-1 arm's reservation fits under (input only, at 0.24)
// holds the chat arm back, and the arm is not called.
func TestChatArm_ReservationHoldsTheOutputAgainstTheArmCap(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	e.set(jevtrialsettings.KeyDailyCostCapUSD, "1")
	cheap := defaultArm("stage1")
	cheap.DailyCostCapUSD = 0.02
	chat := chatArm("chat")
	chat.DailyCostCapUSD = 0.02 // a chat call is reserved at about 0.1 USD
	stage1 := e.addArm("stage1", answer(okResp(), 3000))
	chatCaller := e.addArm("chat", answer(okResp(), 3000))
	e.enable(cheap, chat)
	e.evaluate()
	if stage1.calls() != 1 {
		t.Errorf("the stage-1 arm was called %d times, want 1 (its reservation is input only)", stage1.calls())
	}
	if chatCaller.calls() != 0 {
		t.Errorf("the chat arm was called %d times, want 0 (its reservation exceeds its 0.02 cap)", chatCaller.calls())
	}
	if got := e.skipReasons("chat"); len(got) != 1 || got[0] != jevtrial.SkipDailyCapArm {
		t.Errorf("chat skips = %v, want [%s]", got, jevtrial.SkipDailyCapArm)
	}
}

// A chat arm of the custom provider gets no routing preference (those are OpenRouter's).
func TestAppWiring_CustomChatArmSendsNoRoutingPreference(t *testing.T) {
	var (
		mu   sync.Mutex
		body string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(raw)
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	callers := trial.NewCallers(trial.Keys{Custom: trial.Endpoint{BaseURL: srv.URL, APIKey: "sk-custom"}})
	arm := chatArm("custom-chat")
	arm.Provider = jevtrialsettings.ProviderCustom
	_, _, _, _ = callers(arm).Trader(context.Background(), jev.TraderRequest{})
	mu.Lock()
	defer mu.Unlock()
	if body == "" || strings.Contains(body, `"provider"`) || !strings.Contains(body, `"messages"`) {
		t.Errorf("custom chat body = %.200s, want chat messages and no provider object", body)
	}
}

// With the default caps (0.10 overall, 0.05 per arm) a chat arm that did not raise its
// own is held back by its reservation: no call, and a skip row says which cap.
func TestChatArm_DefaultCapsHoldItBackAndSaySo(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	chat := chatArm("chat")
	chat.DailyCostCapUSD = 0 // the default, 0.05
	caller := e.addArm("chat", answer(okResp(), 3000))
	e.enable(chat)
	e.evaluate()
	if caller.calls() != 0 {
		t.Errorf("the chat arm was called %d times under the default caps, want 0", caller.calls())
	}
	if got := e.skipReasons("chat"); len(got) != 1 || got[0] != jevtrial.SkipDailyCapOverall {
		t.Errorf("chat skips = %v, want [%s]", got, jevtrial.SkipDailyCapOverall)
	}
}
