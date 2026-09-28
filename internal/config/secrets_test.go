package config_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestLoadSecrets_ReturnsValuesFromEnvironment(t *testing.T) {
	t.Setenv(config.EnvJevAPIKey, "jev-key")
	t.Setenv(config.EnvJevBaseURL, "https://jev.example.com")
	t.Setenv(config.EnvKabuAPIPassword, "kabu-pass")

	secrets, err := config.LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
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

func TestLoadSecrets_ReturnsErrorNamingEveryMissingVariable(t *testing.T) {
	t.Setenv(config.EnvJevAPIKey, "")
	t.Setenv(config.EnvJevBaseURL, "")
	t.Setenv(config.EnvKabuAPIPassword, "")

	_, err := config.LoadSecrets()
	if err == nil {
		t.Fatal("LoadSecrets: expected an error when all variables are unset, got nil")
	}
	for _, envVar := range []string{config.EnvJevAPIKey, config.EnvJevBaseURL, config.EnvKabuAPIPassword} {
		if !strings.Contains(err.Error(), envVar) {
			t.Errorf("LoadSecrets error %q does not mention missing variable %q", err, envVar)
		}
	}
}

func TestLoadSecrets_ReturnsErrorForPartiallyMissingVariables(t *testing.T) {
	t.Setenv(config.EnvJevAPIKey, "jev-key")
	t.Setenv(config.EnvJevBaseURL, "")
	t.Setenv(config.EnvKabuAPIPassword, "kabu-pass")

	_, err := config.LoadSecrets()
	if err == nil {
		t.Fatal("LoadSecrets: expected an error when JEV_BASE_URL is unset, got nil")
	}
	if !strings.Contains(err.Error(), config.EnvJevBaseURL) {
		t.Errorf("LoadSecrets error %q does not mention missing variable %q", err, config.EnvJevBaseURL)
	}
	if strings.Contains(err.Error(), config.EnvJevAPIKey) {
		t.Errorf("LoadSecrets error %q unexpectedly mentions present variable %q", err, config.EnvJevAPIKey)
	}
}

func TestLoadSecrets_SlackWebhookURLIsOptional(t *testing.T) {
	t.Setenv(config.EnvJevAPIKey, "jev-key")
	t.Setenv(config.EnvJevBaseURL, "https://jev.example.com")
	t.Setenv(config.EnvKabuAPIPassword, "kabu-pass")
	t.Setenv(config.EnvSlackWebhookURL, "")

	secrets, err := config.LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets with SLACK_WEBHOOK_URL unset: %v", err)
	}
	if secrets.SlackWebhookURL != "" {
		t.Errorf("SlackWebhookURL = %q, want empty", secrets.SlackWebhookURL)
	}

	t.Setenv(config.EnvSlackWebhookURL, "https://hooks.slack.com/services/T/B/X")
	secrets, err = config.LoadSecrets()
	if err != nil {
		t.Fatalf("LoadSecrets: %v", err)
	}
	if secrets.SlackWebhookURL != "https://hooks.slack.com/services/T/B/X" {
		t.Errorf("SlackWebhookURL = %q, want the configured webhook URL", secrets.SlackWebhookURL)
	}
}
