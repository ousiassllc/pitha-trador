package trialwire_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// storedState is a saved jev_decisions.state_json whose key order and unknown
// key a decode and re-encode through jev.ScoutState would change.
const storedState = `{"zzz_unknown_future_field":{"a":1},"symbol":"7203","price":1234.5,"timestamp":"2026-10-05T01:02:03Z"}`

// TraderRaw sends the saved input byte for byte under state.market, with an
// empty similar-case context (no data newer than the decision is added).
func TestTraderRaw_SendsTheSavedInputAsIs(t *testing.T) {
	server, got := newServer(t, http.StatusOK, trialResponse)
	client := trialwire.New(trialwire.Config{BaseURL: server.URL, APIKey: "test-key-xxxx", Model: "vendor/m"})

	resp, _, requestBytes, err := client.TraderRaw(context.Background(), jev.TraderQuestionVersion, json.RawMessage(storedState))
	if err != nil {
		t.Fatalf("TraderRaw: %v", err)
	}
	if resp.Direction != "LONG" || requestBytes == 0 {
		t.Errorf("response direction = %q, request bytes = %d", resp.Direction, requestBytes)
	}
	var state struct {
		Market           json.RawMessage `json:"market"`
		SimilarPastCases json.RawMessage `json:"similar_past_cases"`
	}
	if err := json.Unmarshal(got.body["state"], &state); err != nil {
		t.Fatalf("decode state %s: %v", got.body["state"], err)
	}
	if string(state.Market) != storedState {
		t.Errorf("state.market = %s, want the saved input %s", state.Market, storedState)
	}
	if string(state.SimilarPastCases) != `{}` {
		t.Errorf("similar_past_cases = %s, want the empty context {}", state.SimilarPastCases)
	}
}

// For an input that survives a decode and re-encode, TraderRaw's request equals
// the one Trader sends with an empty RAG context: the replay asks what the live
// trial asks, in the same shape.
func TestTraderRaw_HasTheShapeOfTrader(t *testing.T) {
	req := traderRequest()
	req.State.Price = 1234.5
	stored, err := json.Marshal(req.State)
	if err != nil {
		t.Fatal(err)
	}
	liveServer, live := newServer(t, http.StatusOK, trialResponse)
	if _, _, _, err := trialwire.New(trialwire.Config{BaseURL: liveServer.URL}).Trader(context.Background(), req); err != nil {
		t.Fatalf("Trader: %v", err)
	}
	rawServer, raw := newServer(t, http.StatusOK, trialResponse)
	if _, _, _, err := trialwire.New(trialwire.Config{BaseURL: rawServer.URL}).TraderRaw(context.Background(), req.QuestionVersion, stored); err != nil {
		t.Fatalf("TraderRaw: %v", err)
	}
	for _, field := range []string{"state", "questions"} {
		if string(live.body[field]) != string(raw.body[field]) {
			t.Errorf("%s differs:\nTrader    %s\nTraderRaw %s", field, live.body[field], raw.body[field])
		}
	}
}

// RawRequestBytes is the size of the body TraderRaw sends.
func TestRawRequestBytes_EqualsTheSentBody(t *testing.T) {
	server, _ := newServer(t, http.StatusOK, trialResponse)
	client := trialwire.New(trialwire.Config{BaseURL: server.URL, Model: "vendor/m"})
	_, _, sent, err := client.TraderRaw(context.Background(), jev.TraderQuestionVersion, json.RawMessage(storedState))
	if err != nil {
		t.Fatalf("TraderRaw: %v", err)
	}
	size, err := trialwire.RawRequestBytes("vendor/m", nil, jev.TraderQuestionVersion, json.RawMessage(storedState))
	if err != nil || size != sent {
		t.Errorf("RawRequestBytes = %d, %v, want the sent %d", size, err, sent)
	}
}

// An unusable input or version sends nothing.
func TestTraderRaw_SendsNothingForABadInputOrVersion(t *testing.T) {
	server, got := newServer(t, http.StatusOK, trialResponse)
	client := trialwire.New(trialwire.Config{BaseURL: server.URL, Model: "vendor/m"})
	if _, _, _, err := client.TraderRaw(context.Background(), jev.TraderQuestionVersion, json.RawMessage(`{not json`)); err == nil {
		t.Error("TraderRaw accepted an input that is not JSON")
	}
	if _, _, _, err := client.TraderRaw(context.Background(), "no-such-version", json.RawMessage(storedState)); err == nil {
		t.Error("TraderRaw accepted an unregistered question version")
	}
	if n := got.calls.Load(); n != 0 {
		t.Errorf("%d requests were sent, want 0", n)
	}
}
