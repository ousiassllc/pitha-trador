package jevtrialroute_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/trialkeys"
)

func (a *app) rows(query string) int {
	var n int
	if err := a.db.QueryRow(query).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

// Issue #1220: saving the trial form leaves a mcp_settings_audit row (actor gui, before and
// after) for each of the six whitelisted keys that changed, and none for a rejected form,
// an unchanged save or the keys that are not on the whitelist.
func TestSavingTheTrialFormIsAudited(t *testing.T) {
	a := newApp(t)
	rows := func(where string) int {
		return a.rows(`SELECT COUNT(*) FROM mcp_settings_audit WHERE actor = 'gui'` + where)
	}

	// A rejected form (cap above the maximum): 422, no setting, no audit row.
	if rec := a.post("/settings/jev-trial", join(url.Values{"enabled": {"on"}, "cap": {"1000"}}, validRows), true); rec.Code == http.StatusOK {
		t.Fatalf("a cap of 1000 USD was accepted: %d", rec.Code)
	}
	if n := a.rows(`SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Fatalf("a rejected save left %d audit rows", n)
	}

	form := join(url.Values{"enabled": {"on"}, "cap": {"0.5"}}, validRows)
	if rec := a.post("/settings/jev-trial", form, true); rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	for _, key := range []string{jevtrialsettings.KeyEnabled, jevtrialsettings.KeyArms, jevtrialsettings.KeyDailyCostCapUSD} {
		if n := rows(` AND setting_key = '` + key + `'`); n != 1 {
			t.Errorf("%s: %d audit rows after one save, want 1", key, n)
		}
	}
	var before, after *string
	if err := a.db.QueryRow(`SELECT before_value, after_value FROM mcp_settings_audit WHERE setting_key = ?`, jevtrialsettings.KeyDailyCostCapUSD).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if before != nil || after == nil || *after != "0.5" {
		t.Errorf("cap audit row before=%v after=%v, want NULL → 0.5", before, after)
	}
	var armsAfter string
	if err := a.db.QueryRow(`SELECT after_value FROM mcp_settings_audit WHERE setting_key = ?`, jevtrialsettings.KeyArms).Scan(&armsAfter); err != nil || !strings.Contains(armsAfter, `"or-clef-flash"`) {
		t.Errorf("arms audit row after = %q (%v)", armsAfter, err)
	}
	if n := a.rows(`SELECT COUNT(*) FROM mcp_settings_audit WHERE setting_key LIKE 'jev.trial.%halt%' OR setting_key LIKE 'jev.trial.failures%' OR setting_key LIKE 'jev.trial.throttle%'`); n != 0 {
		t.Errorf("the runner's state keys were audited (%d rows)", n)
	}

	// The same form again changes nothing and records nothing; switching off and a blank cap add one row each.
	a.post("/settings/jev-trial", form, true)
	if n := rows(``); n != 3 {
		t.Fatalf("a repeated save: %d audit rows, want 3", n)
	}
	a.post("/settings/jev-trial", join(url.Values{"cap": {""}}, validRows), true)
	if n := rows(` AND setting_key = '` + jevtrialsettings.KeyEnabled + `'`); n != 2 {
		t.Errorf("switching off left %d switch rows, want 2", n)
	}
	if n := rows(` AND action = 'reset' AND setting_key = '` + jevtrialsettings.KeyDailyCostCapUSD + `'`); n != 1 {
		t.Errorf("a blank cap left %d reset rows, want 1", n)
	}
}

// Issue #1220: what the MCP's settings_set writes is what the Settings screen's panel shows, and
// what the screen saves is what settings_get reads.
func TestSettingsScreenAndMCPSeeTheSameValues(t *testing.T) {
	a := newApp(t)
	w, err := mcpset.OpenWriter("sqlite", a.dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	set := func(key, value string) {
		t.Helper()
		if _, err := w.Set(t.Context(), "test-client", key, value); err != nil {
			t.Fatalf("settings_set %s: %v", key, err)
		}
	}
	set(jevtrialsettings.KeyArms, `[{"id":"mcp-arm","provider":"openrouter","model":"vendor/mcp-model","prompt_version":"trader-v3"}]`)
	set(jevtrialsettings.KeyDailyCostCapUSD, "0.4")
	set(jevtrialsettings.KeyEnabled, "true")
	panel := a.get("/settings/jev-trial-panel").Body.String()
	for _, want := range []string{"mcp-arm", "vendor/mcp-model", `data-testid="jev-trial-cap-effective">0.4 USD`, "（保存した値）"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the panel after the MCP's writes lacks %q", want)
		}
	}
	if !regexp.MustCompile(`name="enabled"[^>]*checked|checked[^>]*name="enabled"`).MatchString(panel) {
		t.Error("the panel does not show the trial as on after the MCP turned it on")
	}

	// And the other way: the screen's save is what settings_get reads.
	if rec := a.post("/settings/jev-trial", join(url.Values{"enabled": {"on"}, "cap": {"0.7"}}, validRows), true); rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	store := mcpset.ReadStore{Q: a.db}
	for key, want := range map[string]string{jevtrialsettings.KeyDailyCostCapUSD: "0.7", jevtrialsettings.KeyEnabled: "true"} {
		if v, err := trialkeys.Value(t.Context(), store, key); err != nil || v.Current != want {
			t.Errorf("%s as the MCP reads it = %q (%v), want %q", key, v.Current, err, want)
		}
	}
	if v, _ := trialkeys.Value(t.Context(), store, jevtrialsettings.KeyArms); !strings.Contains(v.Current, `"or-clef-flash"`) || strings.Contains(v.Current, "mcp-arm") {
		t.Errorf("arms as the MCP reads them after the screen's save = %q", v.Current)
	}
}
