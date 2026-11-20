package jevpromptroute_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// Issue #1113: saving the shadow form leaves a mcp_settings_audit row (actor
// gui, before and after) for the switch and for the candidate, a rejected form
// leaves nothing. The panel is unchanged: it shows no audited value.
func TestSavingTheShadowSettingsIsAudited(t *testing.T) {
	a := newApp(t)
	count := func(key string) int {
		return a.rows(`SELECT COUNT(*) FROM mcp_settings_audit WHERE actor = 'gui' AND setting_key = '` + key + `'`)
	}

	// A rejected candidate (the live version): 422, no setting, no audit row.
	rec := a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v3"}}, true)
	if rec.Code == http.StatusOK {
		t.Fatalf("the live version was accepted as a candidate: %d", rec.Code)
	}
	if n := a.rows(`SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Fatalf("a rejected save left %d audit rows", n)
	}
	if _, ok := a.stored(jevshadowsettings.KeyCandidateVersion); ok {
		t.Fatal("a rejected candidate was stored")
	}

	rec = a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"0.5"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	for _, key := range []string{jevshadowsettings.KeyEnabled, jevshadowsettings.KeyCandidateVersion} {
		if n := count(key); n != 1 {
			t.Errorf("%s: %d audit rows after one save, want 1", key, n)
		}
	}
	var before, after *string
	if err := a.db.QueryRow(`SELECT before_value, after_value FROM mcp_settings_audit WHERE setting_key = ?`, jevshadowsettings.KeyCandidateVersion).Scan(&before, &after); err != nil {
		t.Fatal(err)
	}
	if before != nil || after == nil || *after != "trader-v4" {
		t.Errorf("candidate audit row before=%v after=%v, want NULL → trader-v4", before, after)
	}
	if n := count(jevshadowsettings.KeyDailyCostCapUSD); n != 0 {
		t.Errorf("the cost cap was audited (%d rows)", n)
	}

	// Saving the same form again changes nothing and adds nothing; clearing
	// the candidate and the switch adds one row each.
	a.post("/settings/jev-prompt/shadow", url.Values{"enabled": {"on"}, "candidate": {"trader-v4"}, "cap": {"0.5"}}, true)
	if n := a.rows(`SELECT COUNT(*) FROM mcp_settings_audit`); n != 2 {
		t.Fatalf("a repeated save: %d audit rows, want 2", n)
	}
	a.post("/settings/jev-prompt/shadow", url.Values{"candidate": {""}, "cap": {"0.5"}}, true)
	if n := a.rows(`SELECT COUNT(*) FROM mcp_settings_audit WHERE action = 'reset' AND setting_key = '` + jevshadowsettings.KeyCandidateVersion + `'`); n != 1 {
		t.Errorf("clearing the candidate left %d reset rows, want 1", n)
	}
	if n := count(jevshadowsettings.KeyEnabled); n != 2 {
		t.Errorf("switching off left %d switch rows, want 2", n)
	}

}
