package jevflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
)

// jevRequest is the part of a /v1/systemone request the tests inspect.
type jevRequest struct {
	Model     string                     `json:"model"`
	State     json.RawMessage            `json:"state"`
	Questions map[string]json.RawMessage `json:"questions"`
}

// fakeJev is a TypeSafe /v1/systemone stand-in: answer maps the request to
// the wire answers (nil means a 500), and every request is recorded.
type fakeJev struct {
	Client *jev.Client

	mu       sync.Mutex
	requests []jevRequest
	auth     string
}

func newFakeJev(t *testing.T, apiKey string, answer func(jevRequest) map[string]any) *fakeJev {
	t.Helper()
	f := &fakeJev{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jevRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.auth = r.Header.Get("Authorization")
		f.mu.Unlock()
		answers := answer(req)
		if answers == nil {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(jevtest.Response("jev-test-1", answers))
	}))
	t.Cleanup(server.Close)
	f.Client = jev.NewClient(jev.Config{BaseURL: server.URL, APIKey: apiKey, MaxAttempts: 1})
	return f
}

func (f *fakeJev) calls() []jevRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jevRequest(nil), f.requests...)
}

// An unconfigured override client: Luna/Sol/Opus then fall back to Jev.
func noOverride(label string) *assist.Client { return assist.NewClient(assist.Config{Label: label}) }

func TestLuna_Classify_DefaultsToJevChoiceQuestions(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any {
		return map[string]any{
			"sentiment":  jevtest.Choice(domain.NewsSentimentBullish, 0.9),
			"event_type": jevtest.Choice(domain.NewsEventGuidance, 0.8),
		}
	})
	luna := assist.NewLuna(noOverride("luna"), assist.WithJev(f.Client))

	if !luna.Configured() {
		t.Fatal("Luna.Configured() = false, want true with only Jev configured")
	}
	got, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203", Headline: "業績予想の修正に関するお知らせ", Body: "TDnet 適時開示: https://example.com/x.pdf"})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	want := assist.Classification{Sentiment: domain.NewsSentimentBullish, EventType: domain.NewsEventGuidance, Summary: "業績予想の修正に関するお知らせ"}
	if got != want {
		t.Errorf("Classify = %+v, want %+v (summary is the headline: Jev returns no free text)", got, want)
	}
	reqs := f.calls()
	if len(reqs) != 1 || reqs[0].Model != jev.DefaultModel || f.auth != "Bearer jev-key" {
		t.Fatalf("Jev requests = %+v auth=%q, want one call with the Jev model and key", reqs, f.auth)
	}
	var types []string
	for _, id := range []string{"sentiment", "event_type"} {
		var q struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(reqs[0].Questions[id], &q)
		types = append(types, q.Type)
	}
	if types[0] != "choice" || types[1] != "choice" {
		t.Errorf("question types = %v, want both choice (Jev returns structured answers only)", types)
	}
}

func TestLuna_Classify_OverrideWinsAndJevIsNotCalled(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any { return nil })
	override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(assist.Classification{Sentiment: "bearish", EventType: "規制", Summary: "own api"})
	}))
	t.Cleanup(override.Close)
	luna := assist.NewLuna(assist.NewClient(assist.Config{Label: "luna", BaseURL: override.URL, MaxAttempts: 1}), assist.WithJev(f.Client))

	got, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"})
	if err != nil || got.Summary != "own api" {
		t.Fatalf("Classify = %+v, %v; want the override API's answer", got, err)
	}
	if n := len(f.calls()); n != 0 {
		t.Errorf("Jev was called %d time(s) although LUNA_BASE_URL is set", n)
	}
}

func TestRoles_WithoutJevKeyOrOverrideAreDisabledNotFailing(t *testing.T) {
	f := newFakeJev(t, "", func(jevRequest) map[string]any { return nil })
	luna := assist.NewLuna(noOverride("luna"), assist.WithJev(f.Client))
	sol := assist.NewSol(noOverride("sol"), assist.WithJev(f.Client))
	opus := assist.NewOpus(noOverride("opus"), assist.WithJev(f.Client))

	if luna.Configured() {
		t.Error("Luna.Configured() = true without a Jev key or override")
	}
	if _, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"}); !errors.Is(err, assist.ErrNotConfigured) {
		t.Errorf("Luna error = %v, want ErrNotConfigured", err)
	}
	if _, _, err := sol.Analyze(context.Background(), solInput()); !errors.Is(err, assist.ErrNotConfigured) {
		t.Errorf("Sol error = %v, want ErrNotConfigured", err)
	}
	if _, _, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5)); !errors.Is(err, assist.ErrNotConfigured) {
		t.Errorf("Opus error = %v, want ErrNotConfigured", err)
	}
	if n := len(f.calls()); n != 0 {
		t.Errorf("Jev was called %d time(s) while unconfigured", n)
	}
}

func TestRoles_NilJevBackendIsDisabled(t *testing.T) {
	if assist.NewLuna(noOverride("luna")).Configured() {
		t.Error("Luna without a backend reports Configured")
	}
}

func TestAsk_FailuresDoNotAffectJevHealth(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any { return nil })
	luna := assist.NewLuna(noOverride("luna"), assist.WithJev(f.Client))
	for range 30 {
		if _, err := luna.Classify(context.Background(), assist.NewsItem{Symbol: "7203"}); err == nil {
			t.Fatal("Classify succeeded against a failing Jev")
		}
	}
	if healthy, _ := f.Client.Healthy(context.Background()); !healthy {
		t.Error("auxiliary Luna failures tripped the Jev health signal behind the jev_api_down Kill Switch")
	}
}
