package secretsflow_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// issue #1059: the model-comparison trial's OpenRouter keys are optional and
// independent of the live Jev keys. They must never become setup-required, or
// a machine without a trial would be sent to /setup.
func TestOpenRouterKeys_AreOptionalNeverRequired(t *testing.T) {
	for _, key := range []string{config.KeyOpenRouterAPIKey, config.KeyOpenRouterBaseURL} {
		if slices.Contains(config.RequiredSecretKeys(), key) {
			t.Errorf("%s is in RequiredSecretKeys, want optional", key)
		}
		if !slices.Contains(config.OptionalSecretKeys(), key) || !config.IsAllowedSecretKey(key) {
			t.Errorf("%s is not an allowed optional key", key)
		}
	}
	if got, want := config.RequiredSecretKeys(), []string{config.KeyJevAPIKey, config.KeyKabuAPIPassword}; !slices.Equal(got, want) {
		t.Errorf("RequiredSecretKeys() = %v, want %v", got, want)
	}
	if got := config.KeyOpenRouterAPIKey + config.KeyOpenRouterBaseURL; got != "OPENROUTER_API_KEYOPENROUTER_BASE_URL" {
		t.Errorf("OpenRouter key names changed: %q", got)
	}
	if !config.IsURLSecretKey(config.KeyOpenRouterBaseURL) || config.IsURLSecretKey(config.KeyOpenRouterAPIKey) {
		t.Errorf("OPENROUTER_BASE_URL must be the only OpenRouter URL key")
	}
	if got := config.RequiredSetup(config.BrokerSettings{Provider: config.BrokerKabu}).SecretKeys; slices.Contains(got, config.KeyOpenRouterAPIKey) {
		t.Errorf("Setup Guard requires %s: %v", config.KeyOpenRouterAPIKey, got)
	}
}

func TestLoadSecretsFromDB_OpenRouterKeysAreOptionalAndLoaded(t *testing.T) {
	repo := fakeSecretsRepo{
		config.KeyJevAPIKey:       "jev-key",
		config.KeyKabuAPIPassword: "kabu-pass",
	}
	secrets, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if len(missing) != 0 || secrets.OpenRouterAPIKey != "" || secrets.OpenRouterBaseURL != "" {
		t.Errorf("unset OpenRouter keys: missing = %v, secrets = %+v; want none reported and empty values", missing, secrets)
	}

	repo[config.KeyOpenRouterAPIKey] = "sk-test"
	repo[config.KeyOpenRouterBaseURL] = "https://openrouter.example.com/api"
	secrets, _, err = config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if secrets.OpenRouterAPIKey != "sk-test" || secrets.OpenRouterBaseURL != "https://openrouter.example.com/api" {
		t.Errorf("OpenRouter values loaded as %q / %q, want the stored values", secrets.OpenRouterAPIKey, secrets.OpenRouterBaseURL)
	}
	if secrets.JevAPIKey != "jev-key" {
		t.Errorf("JevAPIKey = %q, want the live key unchanged", secrets.JevAPIKey)
	}
}
