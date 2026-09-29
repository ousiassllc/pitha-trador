package config_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// fakeSecretsRepo is a config.SecretsRepository backed by an in-memory
// map, standing in for internal/repository.SecretsRepository (which
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
	want := []string{config.KeyJevAPIKey, config.KeyJevBaseURL, config.KeyKabuAPIPassword}
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
	repo := fakeSecretsRepo{
		config.KeyJevAPIKey:       "jev-key",
		config.KeyKabuAPIPassword: "kabu-pass",
	}

	_, missing, err := config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if len(missing) != 1 || missing[0] != config.KeyJevBaseURL {
		t.Errorf("missing = %v, want [%q]", missing, config.KeyJevBaseURL)
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
	repo[config.KeySolAPIKey] = "sol-key"
	repo[config.KeySolBaseURL] = "https://sol.example.com"
	repo[config.KeyOpusAPIKey] = "opus-key"
	repo[config.KeyOpusBaseURL] = "https://opus.example.com"
	secrets, _, err = config.LoadSecretsFromDB(context.Background(), repo)
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	if secrets.LunaAPIKey != "luna-key" || secrets.LunaBaseURL != "https://luna.example.com" ||
		secrets.NewsFeedURL != "https://news.example.com/feed" || secrets.NewsFeedAPIKey != "news-key" ||
		secrets.SolAPIKey != "sol-key" || secrets.SolBaseURL != "https://sol.example.com" ||
		secrets.OpusAPIKey != "opus-key" || secrets.OpusBaseURL != "https://opus.example.com" {
		t.Errorf("secrets = %+v, want the configured Luna/News Feed values", secrets)
	}
}
