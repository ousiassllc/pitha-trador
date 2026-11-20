package airole_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
)

type store map[string]string

func (s store) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

type failingStore struct{}

func (failingStore) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("database is locked")
}

var secrets = config.Secrets{
	JevAPIKey: "jev-key", JevBaseURL: "https://jev.example", JevModel: "jev-custom",
	OpenRouterAPIKey: "or-key", OpenRouterBaseURL: "https://or.example/api",
}

// Nothing stored: every role keeps the connection it had before the layer.
func TestResolveDefaultsKeepTheConnectionsRolesAlwaysHad(t *testing.T) {
	jev := airole.Resolve(airole.RoleJev, airole.DefaultSetting(), secrets)
	want := airole.Connection{Role: "jev", Provider: "custom", BaseURL: "https://jev.example", APIKey: "jev-key", Model: "jev-custom"}
	if jev != want {
		t.Errorf("jev default = %+v, want %+v", jev, want)
	}
	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		c := airole.Resolve(role, airole.DefaultSetting(), secrets)
		if !c.Delegated || c.BaseURL != "" || c.APIKey != "" || c.Model != "" {
			t.Errorf("%s default = %+v, want delegated to the role's own override / Jev with nothing of its own", role, c)
		}
	}
}

func TestResolveOpenRouterUsesTheOpenRouterKeyURLAndTheRolesModel(t *testing.T) {
	for _, role := range airole.Roles() {
		c := airole.Resolve(role, airole.Setting{Provider: airole.ProviderOpenRouter, Model: "vendor/model"}, secrets)
		want := airole.Connection{Role: role, Provider: "openrouter", BaseURL: "https://or.example/api", APIKey: "or-key", Model: "vendor/model"}
		if c != want {
			t.Errorf("%s = %+v, want %+v", role, c, want)
		}
	}
	c := airole.Resolve(airole.RoleJev, airole.Setting{Provider: airole.ProviderOpenRouter, Model: "m"}, config.Secrets{})
	if c.BaseURL != airole.DefaultOpenRouterBaseURL {
		t.Errorf("unset OPENROUTER_BASE_URL gave %q, want the public endpoint", c.BaseURL)
	}
}

// A role's own model beats JEV_MODEL for the custom provider; without one JEV_MODEL stays.
func TestResolveJevCustomModelOverride(t *testing.T) {
	c := airole.Resolve(airole.RoleJev, airole.Setting{Provider: airole.ProviderCustom, Model: "mine"}, secrets)
	if c.Model != "mine" || c.APIKey != "jev-key" || c.BaseURL != "https://jev.example" {
		t.Errorf("connection = %+v", c)
	}
}

func TestNormalizeRejectsWhatCannotBeCalled(t *testing.T) {
	for name, tc := range map[string]struct {
		role string
		set  airole.Setting
	}{
		"unknown role":                    {"nobody", airole.Setting{Provider: "custom"}},
		"unknown provider":                {"jev", airole.Setting{Provider: "chatgpt"}},
		"empty provider":                  {"jev", airole.Setting{}},
		"openrouter without a model":      {"sol", airole.Setting{Provider: "openrouter"}},
		"model with a space":              {"sol", airole.Setting{Provider: "openrouter", Model: "a b"}},
		"model with a newline":            {"sol", airole.Setting{Provider: "openrouter", Model: "a\nb"}},
		"model too long":                  {"sol", airole.Setting{Provider: "openrouter", Model: strings.Repeat("a", airole.MaxModelLen+1)}},
		"custom model for a non-Jev role": {"sol", airole.Setting{Provider: "custom", Model: "x"}},
	} {
		if _, err := airole.Normalize(tc.role, tc.set); err == nil {
			t.Errorf("%s was accepted", name)
		} else if _, ok := err.(*airole.InvalidError); !ok {
			t.Errorf("%s: error %T is not an *InvalidError", name, err)
		}
	}
	got, err := airole.Normalize("jev", airole.Setting{Provider: "custom", Model: "  m  "})
	if err != nil || got.Model != "m" {
		t.Errorf("trimmed model = %+v, %v", got, err)
	}
}

func TestEncodeParseRoundTripAndStrictness(t *testing.T) {
	value, err := airole.Encode("jev", airole.Setting{Provider: "openrouter", Model: "a/b"})
	if err != nil || value != `{"provider":"openrouter","model":"a/b"}` {
		t.Fatalf("Encode = %q, %v", value, err)
	}
	got, err := airole.Parse("jev", value)
	if err != nil || got != (airole.Setting{Provider: "openrouter", Model: "a/b"}) {
		t.Fatalf("Parse = %+v, %v", got, err)
	}
	for _, bad := range []string{``, `[]`, `{"provider":"custom","endpoint":"x"}`, `{"provider":"custom"} {}`, `{"provider":"openrouter"}`} {
		if _, err := airole.Parse("jev", bad); err == nil {
			t.Errorf("Parse(%q) was accepted", bad)
		}
	}
}

// A damaged or unreadable stored value falls back to the default (the
// connection the role had before), never to another provider, and says why.
func TestLoadFallsBackToTheDefaultAndReportsDamage(t *testing.T) {
	l := airole.Load(context.Background(), store{
		airole.Key("jev"): `{"provider":"openrouter","model":"a/b"}`,
		airole.Key("sol"): `not json`,
	})
	if got := l.Settings["jev"]; got != (airole.Setting{Provider: "openrouter", Model: "a/b"}) || !l.Stored["jev"] {
		t.Errorf("jev = %+v stored=%v", got, l.Stored["jev"])
	}
	if got := l.Settings["sol"]; got != airole.DefaultSetting() || l.Stored["sol"] || l.Damaged["sol"] == "" {
		t.Errorf("sol = %+v stored=%v damaged=%q", got, l.Stored["sol"], l.Damaged["sol"])
	}
	if got := l.Settings["opus"]; got != airole.DefaultSetting() || l.Damaged["opus"] != "" {
		t.Errorf("opus = %+v damaged=%q", got, l.Damaged["opus"])
	}
	l = airole.Load(context.Background(), failingStore{})
	for _, role := range airole.Roles() {
		if l.Settings[role] != airole.DefaultSetting() || l.Damaged[role] == "" {
			t.Errorf("%s with an unreadable store = %+v damaged=%q", role, l.Settings[role], l.Damaged[role])
		}
	}
}

func TestKeysAreOnlyRoleKeys(t *testing.T) {
	if !airole.IsKey("ai.role.jev") || airole.IsKey("ai.role.") || airole.IsKey("ai.role.other") || airole.IsKey("jev.trial.arms") {
		t.Error("IsKey does not match exactly the role keys")
	}
	if !airole.IsLive("jev") || airole.IsLive("sol") {
		t.Error("only jev is live")
	}
}
