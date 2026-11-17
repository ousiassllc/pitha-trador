package trialflow_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

// Issue #1080: the provider an arm names picks the connection it calls. An
// OpenRouter arm is called with the OpenRouter key and model id at the OpenRouter
// URL and never sees the 独自 API key; a custom arm is called at the 独自 API URL
// with that key and no OpenRouter routing preference. Both are mock servers.
func TestAppWiring_ArmProviderPicksTheConnection(t *testing.T) {
	const orKey, customKey = "or-key-for-test", "custom-key-for-test"
	answer := jev.TraderResponse{Direction: domain.JevDirectionLong, ModelID: "served"}
	orAPI := &apiRecorder{handler: jevtest.TraderHandler(answer)}
	customAPI := &apiRecorder{handler: jevtest.TraderHandler(answer)}
	orServer, customServer := httptest.NewServer(orAPI), httptest.NewServer(customAPI)
	t.Cleanup(orServer.Close)
	t.Cleanup(customServer.Close)

	tr, settings, rows, instID, _ := appTrader(t, trial.Keys{
		APIKey: orKey, BaseURL: orServer.URL,
		Custom: trial.Endpoint{BaseURL: customServer.URL, APIKey: customKey},
	}, nil)
	value, err := jevtrialsettings.EncodeArms([]jevtrialsettings.Arm{
		{ID: "or-arm", Provider: "openrouter", Model: "vendor/or-model", PromptVersion: "trader-v3"},
		{ID: "custom-arm", Provider: "custom", Model: "custom-model", PromptVersion: "trader-v3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{jevtrialsettings.KeyArms: value, jevtrialsettings.KeyEnabled: "1"} {
		if err := settings.Set(context.Background(), k, v, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tr.Evaluate(context.Background(), instID, state(time.Now())); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	waitFor(t, "both arms' rows", func() bool { return rows() == 2 })

	if orAPI.requests() != 1 || customAPI.requests() != 1 {
		t.Fatalf("requests: openrouter=%d custom=%d, want 1 each", orAPI.requests(), customAPI.requests())
	}
	if got := orAPI.headers[0].Get("Authorization"); got != "Bearer "+orKey {
		t.Errorf("OpenRouter arm Authorization = %q, want the OpenRouter key", got)
	}
	if !strings.Contains(orAPI.bodies[0], `"model":"vendor/or-model"`) || !strings.Contains(orAPI.bodies[0], `"data_collection":"deny"`) {
		t.Errorf("OpenRouter arm body = %s", orAPI.bodies[0])
	}
	if got := customAPI.headers[0].Get("Authorization"); got != "Bearer "+customKey {
		t.Errorf("custom arm Authorization = %q, want the custom provider's key", got)
	}
	if !strings.Contains(customAPI.bodies[0], `"model":"custom-model"`) || strings.Contains(customAPI.bodies[0], `"provider"`) {
		t.Errorf("custom arm body = %s, want its model and no routing preference", customAPI.bodies[0])
	}
	for _, h := range orAPI.headers {
		if strings.Contains(h.Get("Authorization"), customKey) {
			t.Error("the custom provider's key reached the OpenRouter server")
		}
	}
}

// A custom arm without a custom connection (no key) is not called, like an
// OpenRouter arm without a key.
func TestAppWiring_CustomArmWithoutAKeyIsNotConfigured(t *testing.T) {
	arm := jevtrialsettings.Arm{ID: "c", Provider: "custom", Model: "m", PromptVersion: "trader-v3"}
	if trial.NewCallers(trial.Keys{APIKey: "or-key"})(arm).Configured() {
		t.Error("a custom arm is Configured with only an OpenRouter key")
	}
	if !trial.NewCallers(trial.Keys{Custom: trial.Endpoint{BaseURL: "http://x", APIKey: "k"}})(arm).Configured() {
		t.Error("a custom arm is not Configured with a custom key")
	}
}

func TestCustomArmCannotCarryOpenRouterRouting(t *testing.T) {
	for name, arm := range map[string]jevtrialsettings.Arm{
		"provider_only":   {ID: "c", Provider: "custom", Model: "m", PromptVersion: "trader-v3", ProviderOnly: []string{"x"}},
		"data_collection": {ID: "c", Provider: "custom", Model: "m", PromptVersion: "trader-v3", DataCollection: "allow"},
		"zdr":             {ID: "c", Provider: "custom", Model: "m", PromptVersion: "trader-v3", ZDR: true},
		"unknown":         {ID: "c", Provider: "chatgpt", Model: "m", PromptVersion: "trader-v3"},
	} {
		if err := arm.Validate(); err == nil {
			t.Errorf("%s: arm accepted", name)
		}
	}
	ok := jevtrialsettings.Arm{ID: "c", Provider: "custom", Model: "m", PromptVersion: "trader-v3"}
	if err := ok.Validate(); err != nil {
		t.Errorf("a plain custom arm was refused: %v", err)
	}
}
