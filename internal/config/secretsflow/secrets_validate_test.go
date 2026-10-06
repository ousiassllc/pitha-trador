package secretsflow_test

import (
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func TestNormalizeSecretValue_TrimsSurroundingWhitespace(t *testing.T) {
	for _, tc := range []struct{ key, raw, want string }{
		{config.KeyJevAPIKey, "  key-123\n", "key-123"},
		{config.KeyKabuAPIPassword, "\tpass \r\n", "pass"},
		{config.KeySlackWebhookURL, "http://127.0.0.1:1 ", "http://127.0.0.1:1"},
		{config.KeyJevBaseURL, "\nhttps://jev.example.com/api\n", "https://jev.example.com/api"},
	} {
		got, err := config.NormalizeSecretValue(tc.key, tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeSecretValue(%s, %q) = %q, %v; want %q, nil", tc.key, tc.raw, got, err, tc.want)
		}
	}
}

func TestNormalizeSecretValue_BlankIsErrEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t "} {
		if _, err := config.NormalizeSecretValue(config.KeyJevAPIKey, raw); !errors.Is(err, config.ErrEmptySecretValue) {
			t.Errorf("NormalizeSecretValue(%q) err = %v, want ErrEmptySecretValue", raw, err)
		}
	}
}

func TestNormalizeSecretValue_URLKeysRequireHTTPOrHTTPSWithHost(t *testing.T) {
	urlKeys := []string{
		config.KeyJevBaseURL, config.KeySlackWebhookURL, config.KeyLunaBaseURL,
		config.KeyNewsFeedURL, config.KeySolBaseURL, config.KeyOpusBaseURL,
	}
	for _, key := range urlKeys {
		for _, ok := range []string{"http://localhost:8080", "https://example.com/path?x=1", "HTTPS://Example.com"} {
			if _, err := config.NormalizeSecretValue(key, ok); err != nil {
				t.Errorf("%s: %q rejected: %v", key, ok, err)
			}
		}
		for _, bad := range []string{
			"notaurl", "ftp://x", "javascript:alert(1)", "//example.com", "http://", "https:///path",
			"http://a b.example.com", "http://exa\nmple.com", "http://:8080",
		} {
			if got, err := config.NormalizeSecretValue(key, bad); err == nil || errors.Is(err, config.ErrEmptySecretValue) || got != "" {
				t.Errorf("%s: %q = %q, %v; want a validation error and no value", key, bad, got, err)
			}
		}
	}
}

func TestNormalizeSecretValue_CredentialKeysRejectControlCharacters(t *testing.T) {
	credentialKeys := []string{
		config.KeyJevAPIKey, config.KeyKabuAPIPassword, config.KeyLunaAPIKey,
		config.KeyNewsFeedAPIKey, config.KeySolAPIKey, config.KeyOpusAPIKey, config.KeyJevModel,
	}
	for _, key := range credentialKeys {
		for _, bad := range []string{"ab\ncd", "ab\r\ncd", "ab\tcd", "ab\x00cd", "ab\x7fcd"} {
			if _, err := config.NormalizeSecretValue(key, bad); err == nil || errors.Is(err, config.ErrEmptySecretValue) {
				t.Errorf("%s: %q err = %v, want a control-character error", key, bad, err)
			}
		}
		if got, err := config.NormalizeSecretValue(key, "p@ss w0rd/ü"); err != nil || got != "p@ss w0rd/ü" {
			t.Errorf("%s: printable value = %q, %v; want it kept as is", key, got, err)
		}
	}
}

func TestNormalizeSecretValue_EveryAllowedKeyIsClassified(t *testing.T) {
	urls := 0
	for _, key := range config.AllowedSecretKeys() {
		if config.IsURLSecretKey(key) {
			urls++
		}
	}
	if urls != 6 {
		t.Errorf("URL keys among allowed keys = %d, want 6", urls)
	}
	if config.IsURLSecretKey("NOT_A_KEY") {
		t.Errorf("unknown key classified as URL key")
	}
}

func TestNormalizeSecretValue_NewsFeedEnabledIsOnOrOff(t *testing.T) {
	for raw, want := range map[string]string{"on": "on", " OFF ": "off", "Off": "off"} {
		if got, err := config.NormalizeSecretValue(config.KeyNewsFeedEnabled, raw); err != nil || got != want {
			t.Errorf("NormalizeSecretValue(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, bad := range []string{"yes", "false", "0", "https://example.com"} {
		if got, err := config.NormalizeSecretValue(config.KeyNewsFeedEnabled, bad); err == nil || got != "" {
			t.Errorf("NormalizeSecretValue(%q) = %q, %v; want a validation error", bad, got, err)
		}
	}
}

func TestSecrets_NewsFeedOffOnlyWhenStoredOff(t *testing.T) {
	for enabled, want := range map[string]bool{"": false, "on": false, "off": true} {
		if got := (config.Secrets{NewsFeedEnabled: enabled}).NewsFeedOff(); got != want {
			t.Errorf("NewsFeedOff() with %q = %v, want %v", enabled, got, want)
		}
	}
}
