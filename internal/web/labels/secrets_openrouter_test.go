package labels_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// The OpenRouter keys (issue #1059) are named for the model-comparison trial,
// marked as not the live Jev's, and never shown as the key itself (issue #904).
func TestSecretName_OpenRouterKeysAreNamedInJapaneseWithoutTheKey(t *testing.T) {
	for _, key := range []string{config.KeyOpenRouterAPIKey, config.KeyOpenRouterBaseURL} {
		name := labels.SecretName(key)
		if name == "" || name == labels.SecretName("NOT_A_KEY") {
			t.Errorf("SecretName(%s) = %q, want a dedicated label", key, name)
		}
		if strings.Contains(name, "OPENROUTER") || !strings.Contains(name, "モデル比較") {
			t.Errorf("SecretName(%s) = %q, want a Japanese label naming the model comparison and no key name", key, name)
		}
	}
}
