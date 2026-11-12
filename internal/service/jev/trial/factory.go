package trial

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// armHTTPTimeout is the timeout of each arm's own http.Client.
const armHTTPTimeout = 30 * time.Second

// Keys are the optional OpenRouter settings from the environment
// (OPENROUTER_API_KEY, OPENROUTER_BASE_URL). Both are optional: without
// APIKey no arm is ever called, and the application still starts. APIKey is
// held in memory only and is never logged or stored.
type Keys struct {
	APIKey  string
	BaseURL string
}

// NewCallers returns the production caller factory: one trialwire.Client per
// arm id, built from the arm's configuration (model, routing preference), with
// its own http.Client (an arm never shares a connection pool with another arm
// or with the live Jev). A client is cached per arm id and rebuilt when the arm's
// configuration changes. Without keys.APIKey a client is not Configured, so it
// is never called.
func NewCallers(keys Keys) func(jevtrialsettings.Arm) Caller {
	var (
		mu     sync.Mutex
		cached = map[string]cachedCaller{}
	)
	return func(arm jevtrialsettings.Arm) Caller {
		cfg, err := json.Marshal(arm)
		if err != nil {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if c, ok := cached[arm.ID]; ok && c.config == string(cfg) {
			return c.client
		}
		client := trialwire.New(trialwire.Config{
			BaseURL:    keys.BaseURL,
			APIKey:     keys.APIKey,
			Model:      arm.Model,
			Provider:   providerFor(arm),
			HTTPClient: &http.Client{Timeout: armHTTPTimeout},
		})
		cached[arm.ID] = cachedCaller{config: string(cfg), client: client}
		return client
	}
}

type cachedCaller struct {
	config string
	client *trialwire.Client
}

// providerFor is the routing preference of arm: data collection denied unless
// the arm allows it, and, when it names providers, only those with fallbacks
// off (so a call is never silently served by another provider).
func providerFor(arm jevtrialsettings.Arm) *systemone.Provider {
	p := &systemone.Provider{DataCollection: arm.DataCollectionOrDefault()}
	if len(arm.ProviderOnly) > 0 {
		p.Only = append([]string(nil), arm.ProviderOnly...)
		p.AllowFallbacks = new(false)
	}
	if arm.ZDR {
		p.ZDR = new(true)
	}
	return p
}
