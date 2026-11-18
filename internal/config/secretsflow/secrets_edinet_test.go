package secretsflow_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// EDINET_API_KEY (issue #1096) is an optional credential: it is on the Settings
// allow-list, is not a URL key, never required by the Setup Guard, and is read
// into Secrets.EdinetAPIKey ("" when unset: the recorder then sends no request).
func TestEdinetAPIKeyIsAnOptionalCredential(t *testing.T) {
	key := config.KeyEdinetAPIKey
	if key != "EDINET_API_KEY" || !config.IsAllowedSecretKey(key) || slices.Contains(config.RequiredSecretKeys(), key) || config.IsURLSecretKey(key) {
		t.Fatalf("%s: allowed=%v required=%v url=%v", key, config.IsAllowedSecretKey(key), slices.Contains(config.RequiredSecretKeys(), key), config.IsURLSecretKey(key))
	}
	if got := config.RequiredSetup(config.BrokerSettings{Provider: config.BrokerKabu}).SecretKeys; slices.Contains(got, key) {
		t.Errorf("the Setup Guard requires %s: %v", key, got)
	}
	if _, err := config.NormalizeSecretValue(key, "ab\ncd"); err == nil {
		t.Error("a control character in the key was accepted")
	}

	repo := map[string]string{}
	secrets, _, err := config.LoadSecretsFromDB(context.Background(), fakeRepo(repo))
	if err != nil || secrets.EdinetAPIKey != "" {
		t.Fatalf("unset: key %q, err %v", secrets.EdinetAPIKey, err)
	}
	repo[key] = "test-key-xxxx"
	if secrets, _, err = config.LoadSecretsFromDB(context.Background(), fakeRepo(repo)); err != nil || secrets.EdinetAPIKey != "test-key-xxxx" {
		t.Fatalf("set: key %q, err %v", secrets.EdinetAPIKey, err)
	}
}

type fakeRepo map[string]string

func (r fakeRepo) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := r[key]
	return v, ok, nil
}
