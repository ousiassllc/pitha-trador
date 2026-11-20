package trialflow_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// pathsOf serves every request with 500 and records the paths it was asked for.
func pathsOf(t *testing.T) (baseURL string, paths func() []string) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// The endpoint is chosen per arm; an arm that names none calls /v1/systemone,
// the path the live Jev uses (issue #1064).
func TestAppWiring_ArmEndpointIsChosenPerArm(t *testing.T) {
	base, paths := pathsOf(t)
	callers := trial.NewCallers(trial.Keys{APIKey: "sk-test", BaseURL: base})
	arm := func(id, endpoint string) jevtrialsettings.Arm {
		return jevtrialsettings.Arm{ID: id, Provider: "openrouter", Model: "m", PromptVersion: "trader-v3", Endpoint: endpoint}
	}
	for _, a := range []jevtrialsettings.Arm{
		arm("default", ""),
		arm("explicit-systemone", jevtrialsettings.EndpointSystemOne),
		arm("decisions", jevtrialsettings.EndpointDecisions),
	} {
		if _, _, _, err := callers(a).Trader(context.Background(), jev.TraderRequest{}); err == nil {
			t.Fatalf("%s: the stub answers 500, want an error", a.ID)
		}
	}
	want := []string{"/v1/systemone", "/v1/systemone", "/alpha/decisions"}
	got := paths()
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("paths = %v, want %v", got, want)
			break
		}
	}
}

// Changing an arm's endpoint rebuilds its cached client.
func TestAppWiring_ChangingTheEndpointRebuildsTheClient(t *testing.T) {
	base, paths := pathsOf(t)
	callers := trial.NewCallers(trial.Keys{APIKey: "sk-test", BaseURL: base})
	arm := jevtrialsettings.Arm{ID: "a", Provider: "openrouter", Model: "m", PromptVersion: "trader-v3"}
	_, _, _, _ = callers(arm).Trader(context.Background(), jev.TraderRequest{})
	arm.Endpoint = jevtrialsettings.EndpointDecisions
	_, _, _, _ = callers(arm).Trader(context.Background(), jev.TraderRequest{})
	if got := paths(); len(got) != 2 || got[0] != "/v1/systemone" || got[1] != "/alpha/decisions" {
		t.Errorf("paths = %v, want /v1/systemone then /alpha/decisions", got)
	}
}

// The settings constants (which cannot import the wire layer) are the wire
// layer's paths, and the default is the live Jev's.
func TestEndpointConstantsMatchTheWireLayer(t *testing.T) {
	if jevtrialsettings.EndpointSystemOne != trialwire.DefaultEndpoint {
		t.Errorf("EndpointSystemOne = %q, trialwire.DefaultEndpoint = %q", jevtrialsettings.EndpointSystemOne, trialwire.DefaultEndpoint)
	}
	if trialwire.DefaultEndpoint != jev.Endpoint {
		t.Errorf("trialwire.DefaultEndpoint = %q, jev.Endpoint = %q", trialwire.DefaultEndpoint, jev.Endpoint)
	}
	if jevtrialsettings.EndpointDecisions != trialwire.DecisionsEndpoint {
		t.Errorf("EndpointDecisions = %q, trialwire %q", jevtrialsettings.EndpointDecisions, trialwire.DecisionsEndpoint)
	}
}
