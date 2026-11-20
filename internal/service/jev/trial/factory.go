package trial

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// armHTTPTimeout is the timeout of each arm's own http.Client.
const armHTTPTimeout = 30 * time.Second

// Keys are the connections of the providers an arm can name. APIKey and BaseURL
// are the optional OpenRouter settings (OPENROUTER_API_KEY, OPENROUTER_BASE_URL):
// without APIKey no OpenRouter arm is ever called, and the application still
// starts. Custom is the 独自 API connection for arms of provider "custom"
// (issue #1080); the zero value means such an arm is never called. The keys are
// held in memory only and are never logged or stored.
type Keys struct {
	APIKey  string
	BaseURL string
	Custom  Endpoint
	// Codex is the ChatGPT subscription through the Codex CLI (issue #1081): it
	// holds no key, only where the CLI and its dedicated CODEX_HOME are.
	Codex CodexConn
}

// CodexConn is where the Codex CLI is: Bin is the executable (empty: "codex" on
// PATH), Home the dedicated CODEX_HOME a person logged in to beforehand. Without a
// Home no Codex arm is ever called.
type CodexConn struct {
	Bin  string
	Home string
}

// Endpoint is one provider connection: the API root (never empty for a
// connection that can be called) and its key.
type Endpoint struct {
	BaseURL string
	APIKey  string
}

// secrets are the credential strings of every connection, to be redacted from
// stored error excerpts.
func (k Keys) secrets() []string {
	return []string{k.APIKey, k.Custom.APIKey}
}

// endpointFor is the connection arm calls: its provider's.
func (k Keys) endpointFor(arm jevtrialsettings.Arm) Endpoint {
	if arm.Provider == jevtrialsettings.ProviderCustom {
		return k.Custom
	}
	return Endpoint{BaseURL: k.BaseURL, APIKey: k.APIKey}
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
	codexLogin := codexwire.NewLogin(codexwire.Config{Bin: keys.Codex.Bin, Home: keys.Codex.Home})
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
		if arm.Provider == jevtrialsettings.ProviderCodexChatGPT {
			client := codexwire.New(codexwire.Config{Bin: keys.Codex.Bin, Home: keys.Codex.Home, Model: arm.Model}, codexLogin)
			cached[arm.ID] = cachedCaller{config: string(cfg), client: client}
			return client
		}
		ep := keys.endpointFor(arm)
		client := trialwire.New(trialwire.Config{
			BaseURL:    ep.BaseURL,
			APIKey:     ep.APIKey,
			Model:      arm.Model,
			Endpoint:   arm.Endpoint, // empty: trialwire.DefaultEndpoint (/v1/systemone)
			Provider:   providerFor(arm),
			HTTPClient: &http.Client{Timeout: armHTTPTimeout},
		})
		cached[arm.ID] = cachedCaller{config: string(cfg), client: client}
		return client
	}
}

type cachedCaller struct {
	config string
	client Caller
}

// providerFor is the routing preference of an OpenRouter arm (none for a custom
// one): data collection denied unless
// the arm allows it, and, when it names providers, only those with fallbacks
// off (so a call is never silently served by another provider).
func providerFor(arm jevtrialsettings.Arm) *systemone.Provider {
	if arm.Provider == jevtrialsettings.ProviderCustom {
		return nil // the routing preferences are OpenRouter's
	}
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
