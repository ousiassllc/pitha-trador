package trialquery

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// KeyState tells whether a provider's API key is stored. It is a bool and
// nothing else: the key's value, its prefix and any part of it are never read
// (the statement asks whether a row exists and selects no column of it).
type KeyState struct {
	KeySet bool `json:"key_set"`
}

// ProviderKeys is the stored state of the API keys of the providers an arm can
// name (issue #1220), so an operator who sets arms through the MCP can see
// whether a key is missing. The ChatGPT subscription (codex_chatgpt) has no key:
// its login state is limits.codex.login.
type ProviderKeys struct {
	// OpenRouter is OPENROUTER_API_KEY (arms of provider openrouter).
	OpenRouter KeyState `json:"openrouter"`
	// Custom is JEV_API_KEY (arms of provider custom, the Jev connection).
	Custom KeyState `json:"custom"`
}

func readProviderKeys(ctx context.Context, q query.Q) (ProviderKeys, error) {
	var (
		pk  ProviderKeys
		err error
	)
	if pk.OpenRouter.KeySet, err = secretExists(ctx, q, config.KeyOpenRouterAPIKey); err != nil {
		return ProviderKeys{}, err
	}
	if pk.Custom.KeySet, err = secretExists(ctx, q, config.KeyJevAPIKey); err != nil {
		return ProviderKeys{}, err
	}
	return pk, nil
}

// secretExists reports whether a secrets row for key exists. The value column is
// never selected.
func secretExists(ctx context.Context, q query.Q, key string) (bool, error) {
	var found bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM secrets WHERE key = ?)`, key).Scan(&found); err != nil {
		return false, fmt.Errorf("query: whether a provider key is set: %w", err)
	}
	return found, nil
}
