package airolewire_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/airolewire"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

type settingsStore map[string]string

func (s settingsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

func ask(t *testing.T, a assist.Asker) {
	t.Helper()
	if _, err := a.Ask(context.Background(), "test", nil, nil); err == nil {
		t.Fatal("the mock answers 400, Ask should fail")
	}
}

type servers struct{ jev, sol, or *AIServer }

func newServers(t *testing.T) (servers, config.Secrets) {
	s := servers{NewAIServer(t), NewAIServer(t), NewAIServer(t)}
	return s, config.Secrets{
		JevAPIKey: "fake-jev-key", JevBaseURL: s.jev.URL, JevModel: "jev-custom",
		SolAPIKey: "fake-sol-key", SolBaseURL: s.sol.URL,
		OpenRouterAPIKey: "fake-or-key", OpenRouterBaseURL: s.or.URL,
	}
}

func liveJev(roles airolewire.Roles) *jev.Client {
	c := roles.Connection(airole.RoleJev)
	return jev.NewClient(jev.Config{BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model})
}

// Nothing saved (an existing installation after issue #1080): Jev, Sol/Opus/Luna
// and the trial call the same endpoint, with the same key and model, as before the
// provider layer. The JEV_*/SOL_*/OPENROUTER_* values are only read.
func TestDefaultsCallTheSameEndpointKeyAndModelAsBefore(t *testing.T) {
	srv, secrets := newServers(t)
	roles := airolewire.Resolve(context.Background(), nil, settingsStore{}, secrets)
	live := liveJev(roles)

	// The live Jev: JEV_BASE_URL + /v1/systemone, the JEV key, the JEV model.
	ask(t, live)
	srv.jev.Expect(t, "live Jev", hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})

	// Opus and Luna have no override: they use the live Jev as before.
	for _, role := range []string{airole.RoleOpus, airole.RoleLuna} {
		client, asker := roles.AssistBackend(role, assist.Config{Label: role}, live)
		if client.Configured() || asker != assist.Asker(live) {
			t.Errorf("%s: override configured=%v, fallback is the live Jev=%v", role, client.Configured(), asker == assist.Asker(live))
		}
		ask(t, asker)
		srv.jev.Expect(t, role+" via Jev", hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})
	}

	// Sol with SOL_BASE_URL set: its own API with its own key; Jev is only the fallback.
	client, asker := roles.AssistBackend(airole.RoleSol, assist.Config{Label: "sol", BaseURL: secrets.SolBaseURL, APIKey: secrets.SolAPIKey}, live)
	if !client.Configured() || asker != assist.Asker(live) {
		t.Fatalf("sol default: configured=%v, fallback is Jev=%v", client.Configured(), asker == assist.Asker(live))
	}
	if err := client.PostJSON(context.Background(), assist.SolAnalyzePath, map[string]string{}, &struct{}{}); err == nil {
		t.Fatal("the mock answers 400")
	}
	srv.sol.Expect(t, "Sol API", hit{assist.SolAnalyzePath, "Bearer fake-sol-key", ""})

	// The trial's OpenRouter arm: OPENROUTER_BASE_URL, the OpenRouter key, the arm's model.
	arm := jevtrialsettings.Arm{ID: "clef", Provider: "openrouter", Model: "vendor/clef", PromptVersion: "trader-v3"}
	if _, _, _, err := trial.NewCallers(airolewire.TrialKeys(secrets))(arm).Trader(context.Background(), jev.TraderRequest{}); err == nil {
		t.Fatal("the mock answers 400")
	}
	srv.or.Expect(t, "trial arm", hit{"/v1/systemone", "Bearer fake-or-key", "vendor/clef"})

	for name, s := range map[string]*AIServer{"jev": srv.jev, "sol": srv.sol, "openrouter": srv.or} {
		s.Expect(t, name+" afterwards")
	}
}

// With every role set to OpenRouter, each calls OpenRouter with the OpenRouter key
// and the role's own model id; no request goes to the 独自 API or to a role's own
// override, and a role without the OpenRouter key is skipped, not moved elsewhere.
func TestOpenRouterRolesCallOpenRouterWithItsKeyAndModel(t *testing.T) {
	srv, secrets := newServers(t)
	stored := settingsStore{}
	for _, role := range airole.Roles() {
		v, err := airole.Encode(role, airole.Setting{Provider: airole.ProviderOpenRouter, Model: "vendor/" + role})
		if err != nil {
			t.Fatal(err)
		}
		stored[airole.Key(role)] = v
	}
	roles := airolewire.Resolve(context.Background(), nil, stored, secrets)
	live := liveJev(roles)

	ask(t, live)
	srv.or.Expect(t, "live Jev on OpenRouter", hit{"/v1/systemone", "Bearer fake-or-key", "vendor/jev"})
	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		client, asker := roles.AssistBackend(role, assist.Config{Label: role, BaseURL: secrets.SolBaseURL, APIKey: secrets.SolAPIKey}, live)
		if client.Configured() {
			t.Errorf("%s: the role's own override is still in use on OpenRouter", role)
		}
		ask(t, asker)
		srv.or.Expect(t, role+" on OpenRouter", hit{"/v1/systemone", "Bearer fake-or-key", "vendor/" + role})
	}
	srv.jev.Expect(t, "独自 API must not be called")
	srv.sol.Expect(t, "Sol override must not be called")

	// No OpenRouter key: the role is not configured (it is skipped); nothing is sent anywhere.
	noKey := secrets
	noKey.OpenRouterAPIKey = ""
	roles = airolewire.Resolve(context.Background(), nil, stored, noKey)
	live = liveJev(roles)
	for _, role := range []string{airole.RoleSol, airole.RoleOpus, airole.RoleLuna} {
		if _, asker := roles.AssistBackend(role, assist.Config{Label: role}, live); asker.Configured() {
			t.Errorf("%s on OpenRouter without a key is configured", role)
		}
	}
	if live.Configured() {
		t.Error("the live Jev on OpenRouter without a key is configured (it would fall back to the 独自 API key)")
	}
	srv.jev.Expect(t, "独自 API without a key")
	srv.or.Expect(t, "OpenRouter without a key")
}

// A saved value that cannot be parsed never moves a role to another provider: the
// role runs on the default, which is the connection it had before.
func TestDamagedSettingRunsOnTheDefault(t *testing.T) {
	srv, secrets := newServers(t)
	roles := airolewire.Resolve(context.Background(), nil, settingsStore{airole.Key("jev"): `{"provider":"openrouter"}`}, secrets)
	if c := roles.Connection(airole.RoleJev); c.Provider != airole.ProviderCustom || c.APIKey != "fake-jev-key" {
		t.Fatalf("jev = %+v", c)
	}
	ask(t, liveJev(roles))
	srv.jev.Expect(t, "live Jev", hit{"/v1/systemone", "Bearer fake-jev-key", "jev-custom"})
	srv.or.Expect(t, "OpenRouter")
}

// The Codex CLI and its dedicated CODEX_HOME come from the environment only (no
// secret, nothing in the database); without PITHA_CODEX_HOME no Codex arm is
// Configured, so the person's own Codex home is never used.
func TestTrialKeysCodexConnectionComesFromTheEnvironment(t *testing.T) {
	t.Setenv(airolewire.EnvCodexHome, "")
	t.Setenv(airolewire.EnvCodexBin, "")
	t.Setenv("CODEX_HOME", "/the/persons/own/home") // the parent's CODEX_HOME is not used
	if k := airolewire.TrialKeys(config.Secrets{}); k.Codex != (trial.CodexConn{}) {
		t.Errorf("codex = %+v, want none without the environment", k.Codex)
	}
	arm := jevtrialsettings.Arm{ID: "cx", Provider: jevtrialsettings.ProviderCodexChatGPT, Model: "m", PromptVersion: "trader-v3"}
	if trial.NewCallers(airolewire.TrialKeys(config.Secrets{}))(arm).Configured() {
		t.Error("a Codex arm is Configured without PITHA_CODEX_HOME")
	}
	t.Setenv(airolewire.EnvCodexHome, "/trial/codex-home")
	t.Setenv(airolewire.EnvCodexBin, "/opt/codex")
	k := airolewire.TrialKeys(config.Secrets{})
	if k.Codex != (trial.CodexConn{Bin: "/opt/codex", Home: "/trial/codex-home"}) {
		t.Errorf("codex = %+v", k.Codex)
	}
	if !trial.NewCallers(k)(arm).Configured() {
		t.Error("a Codex arm is not Configured with PITHA_CODEX_HOME")
	}
}

func TestTrialKeys(t *testing.T) {
	k := airolewire.TrialKeys(config.Secrets{OpenRouterAPIKey: "o", OpenRouterBaseURL: "https://or.example", JevAPIKey: "j"})
	if k.APIKey != "o" || k.BaseURL != "https://or.example" || k.Custom != (trial.Endpoint{BaseURL: jev.DefaultBaseURL, APIKey: "j"}) {
		t.Errorf("keys = %+v", k)
	}
	k = airolewire.TrialKeys(config.Secrets{JevBaseURL: "https://jev.example", JevAPIKey: "j"})
	if k.BaseURL != "" || k.Custom.BaseURL != "https://jev.example" {
		t.Errorf("keys = %+v", k)
	}
	if airole.DefaultOpenRouterBaseURL != trialwire.DefaultBaseURL {
		t.Errorf("the roles' OpenRouter default %q differs from the trial's %q", airole.DefaultOpenRouterBaseURL, trialwire.DefaultBaseURL)
	}
}

// The start-up record says what each role runs on (the effective model for the live Jev) and only a change writes a row.
func TestRecordHasTheRunningPairOfEveryRoleOnce(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	repo := system.NewAIRoleRepository(db)
	if err := repo.Save(ctx, "sol", airole.Setting{Provider: "openrouter", Model: "vendor/sol"}); err != nil {
		t.Fatal(err)
	}
	roles := airolewire.Resolve(ctx, db, system.NewRuntimeSettingsRepository(db), config.Secrets{JevAPIKey: "fake-jev-key"})
	roles.Record(ctx)
	roles.Record(ctx)
	want := map[string]airole.Setting{
		"jev":  {Provider: "custom", Model: "jev-latest"},
		"sol":  {Provider: "openrouter", Model: "vendor/sol"},
		"opus": {Provider: "custom"},
		"luna": {Provider: "custom"},
	}
	for role, set := range want {
		if last, ok, err := repo.LastStarted(ctx, role); err != nil || !ok || last.After != set {
			t.Errorf("%s: running = %+v ok=%v err=%v, want %+v", role, last.After, ok, err, set)
		}
	}
	rows, err := repo.History(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	for _, r := range rows {
		if r.Kind == system.AIRoleStarted {
			started++
		}
	}
	if started != len(want) {
		t.Errorf("started rows = %d after two records, want one per role (%d)", started, len(want))
	}
}
