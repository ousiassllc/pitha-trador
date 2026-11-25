package settingstests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Issue #1080: the AI roles' provider and model, the live Jev role's included, can be
// changed on the Settings screen only. With the write switch ON, settings_set refuses
// every key of the provider layer (and the secrets they select between), and nothing is
// stored, audited or recorded.
func TestAIRoleSettingsAreNotWritableFromTheMCP(t *testing.T) {
	srv, w := settingsServer(t, nil) // the write switch is on by default
	keys := []string{
		"ai.role", "ai.role.", "ai.roles", "ai.role.jev.provider", "ai.role.jev.model",
		config.KeyJevModel, config.KeyJevAPIKey, config.KeyOpenRouterAPIKey, config.KeyOpenRouterBaseURL,
	}
	for _, role := range airole.Roles() {
		keys = append(keys, airole.Key(role))
	}
	for _, key := range keys {
		if mcpwritesettings.IsAllowed(key) {
			t.Errorf("%s is on the MCP write whitelist", key)
		}
		for _, value := range []any{`{"provider":"openrouter","model":"vendor/model"}`, "openrouter"} {
			set(srv, t, key, value).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
		}
	}
	for _, key := range mcpwritesettings.Keys() {
		if strings.HasPrefix(key, airole.KeyPrefix) || strings.HasPrefix(key, "ai.") {
			t.Errorf("whitelisted key %s belongs to the provider layer", key)
		}
	}
	if n := count(t, w, `SELECT (SELECT COUNT(*) FROM runtime_settings) + (SELECT COUNT(*) FROM mcp_settings_audit) + (SELECT COUNT(*) FROM ai_role_history)`); n != 0 {
		t.Fatalf("refused writes left %d rows behind", n)
	}
	for _, it := range getState(srv, t).Items {
		if strings.HasPrefix(it.Key, "ai.") {
			t.Errorf("settings_get lists %s", it.Key)
		}
	}
}

type aiRolesView struct {
	Providers []string `json:"providers"`
	Roles     []struct {
		Role             string `json:"role"`
		Live             bool   `json:"live"`
		Source           string `json:"source"`
		ChangeableViaMCP bool   `json:"changeable_via_mcp"`
		Configured       struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"configured"`
		Running *struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"running"`
	} `json:"roles"`
	History []struct {
		Kind   string `json:"kind"`
		Role   string `json:"role"`
		Before *struct {
			Provider string `json:"provider"`
		} `json:"before"`
		After struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"after"`
	} `json:"history"`
}

// ai_roles shows the provider and model of every role and the history; no API key, URL or
// any other secret can appear in it, even when the secrets table holds them.
func TestAIRolesToolReadsTheSettingsAndNoSecrets(t *testing.T) {
	const (
		jevKey = "SENTINEL-JEV-API-KEY-0123456789"
		orKey  = "SENTINEL-OPENROUTER-KEY-0123456789"
		orURL  = "https://sentinel-openrouter.invalid/api"
	)
	srv, w := settingsServer(t, nil)
	for key, value := range map[string]string{config.KeyJevAPIKey: jevKey, config.KeyOpenRouterAPIKey: orKey, config.KeyOpenRouterBaseURL: orURL, config.KeyJevBaseURL: "https://sentinel-jev.invalid"} {
		testkit.MustExec(t, w, `INSERT INTO secrets (key, encrypted_value, updated_at) VALUES (?, ?, '2026-10-18T00:00:00Z')`, key, value)
	}
	testkit.Setting(t, w, airole.Key("jev"), `{"provider":"openrouter","model":"vendor/jev-model"}`)
	testkit.MustExec(t, w, `INSERT INTO ai_role_history (at, kind, actor, role, before_provider, before_model, provider, model) VALUES ('2026-10-17T01:00:00Z', 'saved', 'gui', 'jev', 'custom', '', 'openrouter', 'vendor/jev-model')`)
	testkit.MustExec(t, w, `INSERT INTO ai_role_history (at, kind, actor, role, provider, model) VALUES ('2026-10-17T02:00:00Z', 'started', 'app', 'jev', 'custom', 'jev-latest')`)

	r := srv.call(t, "ai_roles", map[string]any{})
	var v aiRolesView
	r.data(t, "ai_roles", &v)

	if len(v.Roles) != len(airole.Roles()) || len(v.Providers) != len(airole.Providers()) {
		t.Fatalf("view = %+v", v)
	}
	byRole := map[string]int{}
	for i, role := range v.Roles {
		byRole[role.Role] = i
		if role.ChangeableViaMCP {
			t.Errorf("%s says it can be changed through the MCP", role.Role)
		}
		if role.Live != (role.Role == "jev") {
			t.Errorf("%s live = %v", role.Role, role.Live)
		}
	}
	jev := v.Roles[byRole["jev"]]
	if jev.Source != "stored" || jev.Configured.Provider != "openrouter" || jev.Configured.Model != "vendor/jev-model" || jev.Running == nil || jev.Running.Provider != "custom" || jev.Running.Model != "jev-latest" {
		t.Errorf("jev = %+v", jev)
	}
	if sol := v.Roles[byRole["sol"]]; sol.Source != "default" || sol.Configured.Provider != "custom" || sol.Running != nil {
		t.Errorf("sol = %+v", sol)
	}
	if len(v.History) != 2 || v.History[0].Kind != "started" || v.History[1].Kind != "saved" || v.History[1].Before == nil || v.History[1].After.Model != "vendor/jev-model" {
		t.Errorf("history = %+v", v.History)
	}
	for _, secret := range []string{jevKey, orKey, orURL, "sentinel-jev.invalid", "sentinel-openrouter"} {
		if strings.Contains(string(r.body), secret) {
			t.Errorf("ai_roles leaked %q", secret)
		}
	}
	// The read tools next to it do not carry them either.
	for _, tool := range []string{"settings_get", "settings_audit", "settings_diff"} {
		if strings.Contains(string(srv.call(t, tool, map[string]any{}).body), "SENTINEL") {
			t.Errorf("%s leaked a secret", tool)
		}
	}
}

// With nothing stored every role is on the default (the connection it always had), and
// ai_roles never reads the secrets table: no statement it sends names it.
func TestAIRolesToolDefaultsAndNeverTouchesTheSecretsTable(t *testing.T) {
	path, _ := testkit.NewMigratedDB(t)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})
	var v aiRolesView
	srv.call(t, "ai_roles", map[string]any{}).data(t, "ai_roles", &v)
	if len(counter.StatementLog()) == 0 {
		t.Fatal("the counting driver saw no statement, so the secrets check proves nothing")
	}
	for _, stmt := range counter.StatementLog() {
		if strings.Contains(strings.ToLower(stmt), "secrets") {
			t.Errorf("ai_roles sent a statement naming the secrets table: %s", stmt)
		}
	}
	for _, role := range v.Roles {
		if role.Source != "default" || role.Configured.Provider != "custom" || role.Configured.Model != "" || role.Running != nil {
			t.Errorf("%s = %+v, want the default and no start record", role.Role, role)
		}
	}
	if len(v.History) != 0 {
		t.Errorf("history = %+v", v.History)
	}
}
