package settingstests

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// Issue #1220: the write switch, the runner state out of reach, the audit log and the
// API key never returned, for the お試し記録's six operator keys (jevtrial_test.go).

// settings_write_enable off: settings_set refuses every trial key with write_disabled and
// stores nothing; turning it on again restores them.
func TestTrialSettingsSetIsRefusedWhileWritesAreOff(t *testing.T) {
	srv, w := settingsServer(t, nil)
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	before := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`)
	for _, key := range trialKeys {
		set(srv, t, key, "1").wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	}
	if n := len(storedTrial(t, w)); n != 0 {
		t.Errorf("refused writes stored %d trial rows", n)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != before {
		t.Errorf("refused writes added %d audit rows", n-before)
	}
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	set(srv, t, "jev.trial.enabled", "true").data(t, "settings_set", &struct{}{})
}

// The runner's own state is not writable: halt, failures, throttle and login stay key_not_allowed
// even with writes on, and are not listed by settings_get.
func TestTrialSettingsKeepsRunnerStateOutOfReach(t *testing.T) {
	srv, w := settingsServer(t, nil)
	for _, key := range []string{
		jevtrialsettings.KeyHalt, jevtrialsettings.HaltKey("a"), jevtrialsettings.FailuresKey("a"), jevtrialsettings.ThrottleKey("a"),
		jevtrialsettings.KeyCodexHalt, jevtrialsettings.KeyCodexLogin, "jev.trial.", "jev.trial.min_call_interval",
	} {
		set(srv, t, key, 1).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
	}
	if n := count(t, w, `SELECT COUNT(*) FROM runtime_settings`) + count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Fatalf("refused writes left %d rows behind", n)
	}
	for _, it := range getState(srv, t).Items {
		if strings.HasPrefix(it.Key, "jev.trial.") && !slices.Contains(trialKeys, it.Key) {
			t.Errorf("settings_get lists %s", it.Key)
		}
	}
}

// A successful write leaves one audit row (actor mcp) with the values before and after; the
// diff shows the change; a damaged stored value reads as the default with a warning.
func TestTrialSettingsSetIsAuditedAndShowsInTheDiff(t *testing.T) {
	srv, w := settingsServer(t, nil)
	set(srv, t, "jev.trial.daily_cost_cap_usd", 0.2).data(t, "settings_set", &struct{}{})
	set(srv, t, "jev.trial.daily_cost_cap_usd", 0.3).data(t, "settings_set", &struct{}{})
	set(srv, t, "jev.trial.arms", arms(armOR)).data(t, "settings_set", &struct{}{})
	var al auditList
	srv.call(t, "settings_audit", map[string]any{}).data(t, "settings_audit", &al)
	if len(al.Entries) != 3 {
		t.Fatalf("audit rows = %d, want 3: %+v", len(al.Entries), al.Entries)
	}
	if e := al.Entries[1]; e.Actor != "mcp" || e.Action != "set" || e.Key != "jev.trial.daily_cost_cap_usd" || e.Before == nil || *e.Before != "0.2" || e.After == nil || *e.After != "0.3" {
		t.Errorf("cap audit row = %+v", e)
	}
	if e := al.Entries[0]; e.Key != "jev.trial.arms" || e.Before != nil || e.After == nil || !strings.Contains(*e.After, `"or-a"`) {
		t.Errorf("arms audit row = %+v", e)
	}
	var d settingsDiff
	srv.call(t, "settings_diff", map[string]any{"since_id": 0}).data(t, "settings_diff", &d)
	keys := []string{}
	for _, c := range d.Changes {
		keys = append(keys, c.Key)
	}
	if !slices.Contains(keys, "jev.trial.arms") || !slices.Contains(keys, "jev.trial.daily_cost_cap_usd") {
		t.Errorf("diff keys = %v", keys)
	}

	// A damaged row reads as the safe default and says so.
	if _, err := w.Exec(`UPDATE runtime_settings SET value = 'garbage' WHERE key = 'jev.trial.daily_cost_cap_usd'`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES ('jev.trial.enabled', '"yes"', '2026-10-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	st := getState(srv, t)
	if it := st.item(t, "jev.trial.daily_cost_cap_usd"); it.Value != "0.1" || it.Warning == "" {
		t.Errorf("damaged cap = %+v", it)
	}
	if it := st.item(t, "jev.trial.enabled"); it.Value != "false" || it.Warning == "" {
		t.Errorf("damaged switch = %+v", it)
	}
}

// No MCP response or audit row carries an API key's value, whatever is stored in the secrets
// table: the trial keys' reads and writes never touch it.
func TestTrialSettingsNeverReturnAnAPIKeyValue(t *testing.T) {
	const sentinelOR, sentinelJev = "SENTINEL-OPENROUTER-KEY-VALUE", "SENTINEL-JEV-KEY-VALUE"
	srv, w := settingsServer(t, nil)
	for key, v := range map[string]string{config.KeyOpenRouterAPIKey: sentinelOR, config.KeyJevAPIKey: sentinelJev} {
		// The value column is planted directly: the sentinel must never come back out.
		if _, err := w.Exec(`INSERT INTO secrets (key, encrypted_value, updated_at) VALUES (?, ?, '2026-10-01T00:00:00Z')`, key, v); err != nil {
			t.Fatal(err)
		}
	}
	var bodies []string
	add := func(r response) { bodies = append(bodies, string(r.body)) }
	add(srv.call(t, "settings_get", map[string]any{}))
	add(set(srv, t, "jev.trial.arms", arms(armOR)))
	add(set(srv, t, "jev.trial.enabled", "true"))
	add(set(srv, t, "jev.trial.daily_cost_cap_usd", 1000))   // refused
	add(set(srv, t, config.KeyOpenRouterAPIKey, sentinelOR)) // key_not_allowed
	add(set(srv, t, "jev.trial.arms", arms(armOR, armOR)))   // refused
	add(srv.call(t, "settings_audit", map[string]any{}))
	add(srv.call(t, "settings_diff", map[string]any{"since_id": 0}))
	for _, b := range bodies {
		for _, secret := range []string{sentinelOR, sentinelJev, testAPIKey[:12]} {
			if strings.Contains(b, secret) {
				t.Errorf("a response carries %q: %s", secret, b)
			}
		}
	}
	rows, err := w.Query(`SELECT COALESCE(before_value, '') || COALESCE(after_value, '') FROM mcp_settings_audit`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(v, "SENTINEL") {
			t.Errorf("an audit row carries a key value: %q", v)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
