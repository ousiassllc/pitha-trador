package config_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// fakeSecretsRepo is a config.SecretsRepository backed by an in-memory
// map, standing in for internal/repository/system.SecretsRepository (which
// itself requires a SQLite database) in these unit tests.
type fakeSecretsRepo map[string]string

func (f fakeSecretsRepo) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := f[key]
	return value, ok, nil
}

// erroringSecretsRepo always fails, simulating an actual DB error.
type erroringSecretsRepo struct{ err error }

func (e erroringSecretsRepo) Get(context.Context, string) (string, bool, error) {
	return "", false, e.err
}

func TestLoadSecretsFromDB_ReturnsValuesFromRepository(t *testing.T) {
	repo := fakeSecretsRepo{
		config.KeyJevAPIKey:       "jev-key",
		config.KeyJevBaseURL:      "https://jev.example.com",
		config.KeyKabuAPIPassword: "kabu-pass",
	}

	secrets, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing = %v, want empty", missing)
	}
	if secrets.JevAPIKey != "jev-key" {
		t.Errorf("JevAPIKey = %q, want %q", secrets.JevAPIKey, "jev-key")
	}
	if secrets.JevBaseURL != "https://jev.example.com" {
		t.Errorf("JevBaseURL = %q, want %q", secrets.JevBaseURL, "https://jev.example.com")
	}
	if secrets.KabuAPIPassword != "kabu-pass" {
		t.Errorf("KabuAPIPassword = %q, want %q", secrets.KabuAPIPassword, "kabu-pass")
	}
}

func TestLoadSecretsFromDB_ReportsEveryMissingRequiredKey(t *testing.T) {
	secrets, missing, err := config.LoadSecretsFromDB(context.Background(), fakeSecretsRepo{})
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if secrets != (config.Secrets{}) {
		t.Errorf("secrets = %+v, want zero value", secrets)
	}
	want := []string{config.KeyJevAPIKey, config.KeyKabuAPIPassword}
	if len(missing) != len(want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
	for i, key := range want {
		if missing[i] != key {
			t.Errorf("missing[%d] = %q, want %q", i, missing[i], key)
		}
	}
}

func TestLoadSecretsFromDB_ReportsOnlyPartiallyMissingKeys(t *testing.T) {
	repo := fakeSecretsRepo{config.KeyJevAPIKey: "jev-key"}

	_, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if len(missing) != 1 || missing[0] != config.KeyKabuAPIPassword {
		t.Errorf("missing = %v, want [%q]", missing, config.KeyKabuAPIPassword)
	}
}

// Issues #271/#274: JEV_BASE_URL and JEV_MODEL are optional overrides - the
// two credentials alone satisfy the Setup Guard's key set, and a stored
// override is loaded as is.
func TestLoadSecretsFromDB_JevBaseURLAndModelAreOptionalOverrides(t *testing.T) {
	repo := fakeSecretsRepo{
		config.KeyJevAPIKey:       "jev-key",
		config.KeyKabuAPIPassword: "kabu-pass",
	}

	secrets, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing = %v, want empty (JEV_BASE_URL/JEV_MODEL must not count)", missing)
	}
	if secrets.JevBaseURL != "" || secrets.JevModel != "" {
		t.Errorf("unset overrides loaded as BaseURL=%q Model=%q, want empty", secrets.JevBaseURL, secrets.JevModel)
	}

	repo[config.KeyJevBaseURL] = "https://jev.example.com"
	repo[config.KeyJevModel] = "jev-custom"
	secrets, _, err = config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if secrets.JevBaseURL != "https://jev.example.com" || secrets.JevModel != "jev-custom" {
		t.Errorf("overrides loaded as BaseURL=%q Model=%q, want the stored values", secrets.JevBaseURL, secrets.JevModel)
	}
}

func TestRequiredSecretKeys_AreOnlyTheTwoCredentials(t *testing.T) {
	want := []string{config.KeyJevAPIKey, config.KeyKabuAPIPassword}
	if got := config.RequiredSecretKeys(); !slices.Equal(got, want) {
		t.Errorf("RequiredSecretKeys() = %v, want %v", got, want)
	}
	for _, key := range []string{config.KeyJevBaseURL, config.KeyJevModel} {
		if slices.Contains(config.RequiredSecretKeys(), key) || !slices.Contains(config.OptionalSecretKeys(), key) {
			t.Errorf("%s must be optional, not required", key)
		}
	}
}

func TestLoadSecretsFromDB_SlackWebhookURLIsOptional(t *testing.T) {
	repo := fakeSecretsRepo{
		config.KeyJevAPIKey:       "jev-key",
		config.KeyJevBaseURL:      "https://jev.example.com",
		config.KeyKabuAPIPassword: "kabu-pass",
	}

	secrets, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB without SLACK_WEBHOOK_URL: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing = %v, want empty (SLACK_WEBHOOK_URL must not count)", missing)
	}
	if secrets.SlackWebhookURL != "" {
		t.Errorf("SlackWebhookURL = %q, want empty", secrets.SlackWebhookURL)
	}

	repo[config.KeySlackWebhookURL] = "https://hooks.slack.com/services/T/B/X"
	secrets, _, err = config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if secrets.SlackWebhookURL != "https://hooks.slack.com/services/T/B/X" {
		t.Errorf("SlackWebhookURL = %q, want the configured webhook URL", secrets.SlackWebhookURL)
	}
}

func TestLoadSecretsFromDB_PropagatesRepositoryError(t *testing.T) {
	wantErr := errors.New("db is locked")

	_, _, err := config.LoadSecretsFromDB(context.Background(), erroringSecretsRepo{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("LoadSecretsFromDB error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestLoadSecretsFromDB_AIAndNewsFeedKeysAreOptionalAndLoaded(t *testing.T) {
	repo := fakeSecretsRepo{
		config.KeyJevAPIKey:       "jev-key",
		config.KeyJevBaseURL:      "https://jev.example.com",
		config.KeyKabuAPIPassword: "kabu-pass",
	}

	secrets, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing = %v, want empty (Luna/News Feed keys must not count)", missing)
	}
	if secrets.LunaAPIKey != "" || secrets.NewsFeedURL != "" {
		t.Errorf("unset optional keys loaded as %+v, want empty", secrets)
	}

	repo[config.KeyLunaAPIKey] = "luna-key"
	repo[config.KeyLunaBaseURL] = "https://luna.example.com"
	repo[config.KeyNewsFeedURL] = "https://news.example.com/feed"
	repo[config.KeyNewsFeedAPIKey] = "news-key"
	repo[config.KeyNewsFeedEnabled] = "off"
	repo[config.KeySolAPIKey] = "sol-key"
	repo[config.KeySolBaseURL] = "https://sol.example.com"
	repo[config.KeyOpusAPIKey] = "opus-key"
	repo[config.KeyOpusBaseURL] = "https://opus.example.com"
	secrets, _, err = config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if secrets.LunaAPIKey != "luna-key" || secrets.LunaBaseURL != "https://luna.example.com" ||
		secrets.NewsFeedURL != "https://news.example.com/feed" || secrets.NewsFeedAPIKey != "news-key" || !secrets.NewsFeedOff() ||
		secrets.SolAPIKey != "sol-key" || secrets.SolBaseURL != "https://sol.example.com" ||
		secrets.OpusAPIKey != "opus-key" || secrets.OpusBaseURL != "https://opus.example.com" {
		t.Errorf("secrets = %+v, want the configured Luna/News Feed values", secrets)
	}
}

func TestIsAllowedSecretKey_AcceptsEveryConfigKeyAndNothingElse(t *testing.T) {
	for _, key := range []string{
		config.KeyJevAPIKey, config.KeyJevBaseURL, config.KeyJevModel, config.KeyKabuAPIPassword, config.KeySlackWebhookURL,
		config.KeyLunaAPIKey, config.KeyLunaBaseURL, config.KeyNewsFeedURL, config.KeyNewsFeedAPIKey, config.KeyNewsFeedEnabled,
		config.KeySolAPIKey, config.KeySolBaseURL, config.KeyOpusAPIKey, config.KeyOpusBaseURL,
	} {
		if !config.IsAllowedSecretKey(key) {
			t.Errorf("IsAllowedSecretKey(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"", "jev_api_key", "PITHA_ENCRYPTION_KEY", "JEV_API_KEY ", "../JEV_API_KEY"} {
		if config.IsAllowedSecretKey(key) {
			t.Errorf("IsAllowedSecretKey(%q) = true, want false", key)
		}
	}
	if got := len(config.AllowedSecretKeys()); got != 14 {
		t.Errorf("len(AllowedSecretKeys()) = %d, want 14", got)
	}
}
