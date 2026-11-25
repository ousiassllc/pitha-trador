package trialtests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
)

// Issue #1220: jev_trial_cost says per provider whether an API key is stored, as a bool
// only. A stored value (planted in the column the tool must never select) comes back
// nowhere in the response.
func TestTrialCostReportsWhetherEachProviderKeyIsSetAsABoolOnly(t *testing.T) {
	const sentinel = "SENTINEL-PROVIDER-KEY-VALUE"
	srv, w := newServer(t)
	if got := cost(t, srv, period).Limits.ProviderKeys; got.OpenRouter.KeySet || got.Custom.KeySet {
		t.Fatalf("nothing stored: provider_keys = %+v, want both unset", got)
	}
	plant := func(key string) {
		t.Helper()
		if _, err := w.Exec(`INSERT INTO secrets (key, encrypted_value, updated_at) VALUES (?, ?, '2026-10-01T00:00:00Z')`, key, sentinel); err != nil {
			t.Fatal(err)
		}
	}
	plant(config.KeyOpenRouterAPIKey)
	if got := cost(t, srv, period).Limits.ProviderKeys; !got.OpenRouter.KeySet || got.Custom.KeySet {
		t.Errorf("OpenRouter key stored: provider_keys = %+v, want openrouter only", got)
	}
	plant(config.KeyJevAPIKey)
	if got := cost(t, srv, period).Limits.ProviderKeys; got != (trialquery.ProviderKeys{OpenRouter: trialquery.KeyState{KeySet: true}, Custom: trialquery.KeyState{KeySet: true}}) {
		t.Errorf("both keys stored: provider_keys = %+v", got)
	}
	r := srv.call(t, "jev_trial_cost", period)
	body := string(r.body)
	if strings.Contains(body, sentinel) || !strings.Contains(body, `\"provider_keys\":{\"openrouter\":{\"key_set\":true},\"custom\":{\"key_set\":true}}`) {
		t.Errorf("response = %s", body)
	}
}
