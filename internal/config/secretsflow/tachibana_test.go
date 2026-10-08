package secretsflow_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

func tachibanaSecrets(t *testing.T) config.Secrets {
	t.Helper()
	secrets, _, err := config.LoadSecretsFromDB(context.Background(), fakeSecretsRepo{
		config.KeyTachibanaDemoAuthID:         "demo-id",
		config.KeyTachibanaProdAuthID:         "prod-id",
		config.KeyTachibanaDemoSecondPassword: "demo-second",
	})
	if err != nil {
		t.Fatalf("LoadSecretsFromDB: %v", err)
	}
	return secrets
}

func TestSecrets_TachibanaCredentials_DemoCarriesSecondPassword(t *testing.T) {
	got := tachibanaSecrets(t).TachibanaCredentials(config.TachibanaEnvDemo)
	if got.AuthID != "demo-id" || got.SecondPassword != "demo-second" {
		t.Errorf("demo credentials = %+v, want the demo ID and second password", got)
	}
}

// 本番 never carries a 第二暗証番号, even when the デモ one is stored (issue #733):
// there is no production key, and the demo value must not leak across.
func TestSecrets_TachibanaCredentials_ProductionNeverCarriesSecondPassword(t *testing.T) {
	secrets := tachibanaSecrets(t)
	for _, env := range []string{config.TachibanaEnvProduction, "", "unknown"} {
		got := secrets.TachibanaCredentials(env)
		if got.SecondPassword != "" {
			t.Errorf("TachibanaCredentials(%q).SecondPassword = %q, want empty", env, got.SecondPassword)
		}
		if got.AuthID != "prod-id" {
			t.Errorf("TachibanaCredentials(%q).AuthID = %q, want the production ID", env, got.AuthID)
		}
	}
}

func TestRequiredSetup_SwitchesWithBrokerAndEnvironment(t *testing.T) {
	tests := []struct {
		name         string
		broker       config.BrokerSettings
		wantSecrets  []string
		wantSettings []string
	}{
		{"zero value is kabu", config.BrokerSettings{}, []string{config.KeyJevAPIKey, config.KeyKabuAPIPassword}, nil},
		{"kabu", config.BrokerSettings{Provider: config.BrokerKabu}, []string{config.KeyJevAPIKey, config.KeyKabuAPIPassword}, nil},
		{
			"tachibana demo",
			config.BrokerSettings{Provider: config.BrokerTachibana, Tachibana: config.TachibanaSettings{Environment: config.TachibanaEnvDemo}},
			[]string{config.KeyJevAPIKey, config.KeyTachibanaDemoAuthID}, []string{config.KeyTachibanaDemoPrivateKeyPath},
		},
		{
			"tachibana production",
			config.BrokerSettings{Provider: config.BrokerTachibana, Tachibana: config.TachibanaSettings{Environment: config.TachibanaEnvProduction}},
			[]string{config.KeyJevAPIKey, config.KeyTachibanaProdAuthID}, []string{config.KeyTachibanaProdPrivateKeyPath},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := config.RequiredSetup(tc.broker)
			if !slices.Equal(got.SecretKeys, tc.wantSecrets) || !slices.Equal(got.SettingKeys, tc.wantSettings) {
				t.Errorf("RequiredSetup = %+v, want secrets %v settings %v", got, tc.wantSecrets, tc.wantSettings)
			}
			if slices.Contains(got.SecretKeys, "TACHIBANA_PROD_SECOND_PASSWORD") {
				t.Error("a 本番 第二暗証番号 must never be required")
			}
		})
	}
}

func TestNormalizeBrokerSetting(t *testing.T) {
	ok := []struct {
		key, raw string
		want     any
	}{
		{config.KeyBrokerProvider, "Tachibana", "tachibana"},
		{config.KeyBrokerProvider, "kabu", "kabu"},
		{config.KeyTachibanaEnvironment, "PRODUCTION", "production"},
		{config.KeyTachibanaDemoBaseURL, "https://demo.example.com/e_api_v4r11", "https://demo.example.com/e_api_v4r11/"},
		{config.KeyTachibanaProdBaseURL, "https://kabuka.e-shiten.jp/e_api_v4r10//", "https://kabuka.e-shiten.jp/e_api_v4r10/"},
		{config.KeyTachibanaRequestMaxPerSecond, "1", 1},
		{config.KeyTachibanaRequestMaxPerSecond, "10", 10},
		{config.KeyTachibanaReauthTime, "05:30", "05:30"},
		{config.KeyTachibanaReauthTime, "8:00", "08:00"},
		{config.KeyTachibanaReauthTime, "06:15", "06:15"},
	}
	for _, tc := range ok {
		got, err := config.NormalizeBrokerSetting(tc.key, tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeBrokerSetting(%q, %q) = %v, %v; want %v", tc.key, tc.raw, got, err, tc.want)
		}
	}
	bad := []struct{ key, raw string }{
		{config.KeyBrokerProvider, "sbi"},
		{config.KeyTachibanaEnvironment, "staging"},
		{config.KeyTachibanaDemoBaseURL, "http://demo.example.com/"},
		{config.KeyTachibanaDemoBaseURL, "https:///nohost"},
		{config.KeyTachibanaDemoBaseURL, "https://user:pw@demo.example.com/"},
		{config.KeyTachibanaProdBaseURL, "https://kabuka.e-shiten.jp/e_api_v4r10/?x=1"},
		{config.KeyTachibanaRequestMaxPerSecond, "0"},
		{config.KeyTachibanaRequestMaxPerSecond, "11"},
		{config.KeyTachibanaRequestMaxPerSecond, "2.5"},
		{config.KeyTachibanaReauthTime, "05:29"},
		{config.KeyTachibanaReauthTime, "08:01"},
		{config.KeyTachibanaReauthTime, "0535"},
		{config.KeyTachibanaReauthTime, "25:00"},
	}
	for _, tc := range bad {
		if got, err := config.NormalizeBrokerSetting(tc.key, tc.raw); err == nil {
			t.Errorf("NormalizeBrokerSetting(%q, %q) = %v, want an error", tc.key, tc.raw, got)
		}
	}
}

func TestTachibanaSettings_SelectsEnvironmentValues(t *testing.T) {
	s := config.TachibanaSettings{
		Environment: config.TachibanaEnvProduction, DemoBaseURL: "d", ProdBaseURL: "p", DemoPrivateKeyPath: "/d.pem", ProdPrivateKeyPath: "/p.pem",
	}
	if s.BaseURL() != "p" || s.PrivateKeyPath() != "/p.pem" || !s.Production() {
		t.Errorf("production selection = %q %q", s.BaseURL(), s.PrivateKeyPath())
	}
	s.Environment = config.TachibanaEnvDemo
	if s.BaseURL() != "d" || s.PrivateKeyPath() != "/d.pem" || s.Production() {
		t.Errorf("demo selection = %q %q", s.BaseURL(), s.PrivateKeyPath())
	}
}
