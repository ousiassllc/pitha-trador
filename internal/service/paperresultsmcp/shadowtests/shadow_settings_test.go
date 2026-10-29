package shadowtests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

type shadowSettingItem struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Source      string `json:"source"`
	Default     string `json:"default"`
	Description string `json:"description"`
	Constraint  string `json:"constraint"`
}

func shadowItems(t *testing.T, srv *testServer) map[string]shadowSettingItem {
	t.Helper()
	var st struct {
		Items []shadowSettingItem `json:"items"`
	}
	srv.call(t, "settings_get", map[string]any{}).data(t, "settings_get", &st)
	out := map[string]shadowSettingItem{}
	for _, it := range st.Items {
		if strings.HasPrefix(it.Key, "jev.shadow.") {
			out[it.Key] = it
		}
	}
	return out
}

// settings_get lists the shadow switch and candidate - exactly those two of the
// jev.shadow.* keys - with value, source, default, description and the
// accepted values; nothing stored means off and no candidate.
func TestSettingsGetShowsTheShadowSwitchAndCandidate(t *testing.T) {
	srv, _ := shadowServer(t)
	items := shadowItems(t, srv)
	if len(items) != 2 {
		t.Fatalf("settings_get shows %d jev.shadow.* keys, want exactly 2: %+v", len(items), items)
	}
	en, ok := items[jevshadowsettings.KeyEnabled]
	if !ok || en.Value != "false" || en.Default != "false" || en.Source != "default" || en.Description == "" || en.Constraint == "" {
		t.Errorf("enabled = %+v, want off by default with a description and the accepted values", en)
	}
	cand, ok := items[jevshadowsettings.KeyCandidateVersion]
	if !ok || cand.Value != "" || cand.Default != "" || cand.Source != "default" || cand.Description == "" || cand.Constraint == "" {
		t.Errorf("candidate_version = %+v, want unset by default with a description and the accepted values", cand)
	}
}

// The acceptance flow: candidate_version=trader-v5 then enabled=true succeed,
// show up in settings_get and jev_shadow_cost, and leave an audit row each; the
// live version is untouched.
func TestSettingsSetChangesTheShadowCandidateAndSwitch(t *testing.T) {
	srv, w := shadowServer(t)

	var out struct {
		Key    string  `json:"key"`
		Value  string  `json:"value"`
		Before *string `json:"before"`
	}
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.candidate_version", "value": "trader-v5"}).data(t, "settings_set", &out)
	if out.Value != "trader-v5" || out.Before != nil {
		t.Fatalf("candidate set = %+v", out)
	}
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.enabled", "value": "true"}).data(t, "settings_set", &out)
	if out.Value != "true" || out.Before != nil {
		t.Fatalf("enabled set = %+v", out)
	}

	// Stored the way the shadow and the Settings screen read it.
	if v, _ := storedValue(t, w, jevshadowsettings.KeyCandidateVersion); v != `"trader-v5"` {
		t.Errorf("stored candidate = %q", v)
	}
	if v, _ := storedValue(t, w, jevshadowsettings.KeyEnabled); v != "1" {
		t.Errorf("stored switch = %q", v)
	}
	if _, ok := storedValue(t, w, jevshadowsettings.KeyLiveVersion); ok {
		t.Error("the live version was written")
	}

	var c shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &c)
	if !c.Enabled || c.CandidateVersion == nil || *c.CandidateVersion != "trader-v5" || c.LiveVersion != "trader-v3" {
		t.Errorf("jev_shadow_cost: enabled %v candidate %v live %q", c.Enabled, c.CandidateVersion, c.LiveVersion)
	}
	items := shadowItems(t, srv)
	if it := items[jevshadowsettings.KeyEnabled]; it.Value != "true" || it.Source != "stored" {
		t.Errorf("settings_get enabled = %+v", it)
	}
	if it := items[jevshadowsettings.KeyCandidateVersion]; it.Value != "trader-v5" || it.Source != "stored" {
		t.Errorf("settings_get candidate = %+v", it)
	}

	// Switching it off again, with the accepted spellings, is audited too.
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.enabled", "value": 0}).data(t, "settings_set", &out)
	if out.Value != "false" || out.Before == nil || *out.Before != "true" {
		t.Fatalf("enabled off = %+v", out)
	}
	if v, _ := storedValue(t, w, jevshadowsettings.KeyEnabled); v != "0" {
		t.Errorf("stored switch after off = %q", v)
	}

	var al struct {
		Entries []struct {
			Actor  string  `json:"actor"`
			Action string  `json:"action"`
			Key    string  `json:"key"`
			Before *string `json:"before"`
			After  *string `json:"after"`
		} `json:"entries"`
	}
	srv.call(t, "settings_audit", map[string]any{}).data(t, "settings_audit", &al)
	if len(al.Entries) != 3 {
		t.Fatalf("audit entries = %+v, want 3 (newest first: off, on, candidate)", al.Entries)
	}
	off, on, cand := al.Entries[0], al.Entries[1], al.Entries[2]
	if off.Key != jevshadowsettings.KeyEnabled || off.Actor != "mcp" || off.Action != "set" || off.Before == nil || *off.Before != "true" || off.After == nil || *off.After != "false" {
		t.Errorf("off row = %+v", off)
	}
	if on.Key != jevshadowsettings.KeyEnabled || on.Before != nil || on.After == nil || *on.After != "true" {
		t.Errorf("on row = %+v", on)
	}
	if cand.Key != jevshadowsettings.KeyCandidateVersion || cand.Actor != "mcp" || cand.Before != nil || cand.After == nil || *cand.After != "trader-v5" {
		t.Errorf("candidate row = %+v", cand)
	}

	var d struct {
		Changes []struct {
			Key   string `json:"key"`
			After struct{ Value, Source string }
		} `json:"changes"`
	}
	srv.call(t, "settings_diff", map[string]any{"since_id": 0}).data(t, "settings_diff", &d)
	if len(d.Changes) != 2 {
		t.Errorf("settings_diff since the start = %+v, want the candidate and the switch", d.Changes)
	}
}

// A version that is not registered, the live version, a candidate-less
// "on" and a value that is no boolean are refused with the reason, and nothing
// is stored or audited. The same validation the Settings screen uses
// (jevprompt.ValidateCandidate).
func TestSettingsSetRejectsInvalidShadowValues(t *testing.T) {
	srv, w := shadowServer(t)
	reject := func(key string, value any, wantReason string) {
		t.Helper()
		e := srv.call(t, "settings_set", map[string]any{"key": key, "value": value}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
		if d := e.Error.Data["detail"]; !strings.Contains(d, wantReason) {
			t.Errorf("%s=%v: detail %q does not say %q", key, value, d, wantReason)
		}
	}
	reject("jev.shadow.candidate_version", "trader-v999", "登録されていません")
	reject("jev.shadow.candidate_version", "trader-v3", "ライブの版")
	reject("jev.shadow.enabled", "true", "候補の版")
	reject("jev.shadow.enabled", "maybe", "true / false")
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.candidate_version", "value": ""}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.enabled", "value": true}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	for _, key := range []string{jevshadowsettings.KeyEnabled, jevshadowsettings.KeyCandidateVersion} {
		if _, ok := storedValue(t, w, key); ok {
			t.Errorf("a rejected write stored %s", key)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Errorf("rejected writes left %d audit rows", n)
	}

	// After a promotion the old live version is a candidate and the new live one is refused.
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v4"}).data(t, "jev_prompt_promote", &struct{}{})
	reject("jev.shadow.candidate_version", "trader-v4", "ライブの版")
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.candidate_version", "value": "trader-v3"}).data(t, "settings_set", &struct{}{})
}

// Writes off: both keys are refused and nothing is stored; turning writes on
// again lets them through.
func TestSettingsSetOfTheShadowKeysFollowsTheWriteSwitch(t *testing.T) {
	srv, w := shadowServer(t)
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	for key, value := range map[string]any{"jev.shadow.candidate_version": "trader-v5", "jev.shadow.enabled": "false"} {
		srv.call(t, "settings_set", map[string]any{"key": key, "value": value}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	}
	for _, key := range []string{jevshadowsettings.KeyEnabled, jevshadowsettings.KeyCandidateVersion} {
		if _, ok := storedValue(t, w, key); ok {
			t.Errorf("a refused write stored %s", key)
		}
	}
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.candidate_version", "value": "trader-v5"}).data(t, "settings_set", &struct{}{})
}

// A damaged stored row reads as the default (off, no candidate), like the
// shadow itself reads it, and a candidate-less "on" is still refused.
func TestSettingsGetReadsADamagedShadowRowAsTheDefault(t *testing.T) {
	srv, w := shadowServer(t)
	for key, raw := range map[string]string{jevshadowsettings.KeyEnabled: `"yes"`, jevshadowsettings.KeyCandidateVersion: `7`} {
		if _, err := w.Exec(`INSERT INTO runtime_settings (key, value, updated_at) VALUES (?, ?, 't')`, key, raw); err != nil {
			t.Fatal(err)
		}
	}
	items := shadowItems(t, srv)
	if it := items[jevshadowsettings.KeyEnabled]; it.Value != "false" {
		t.Errorf("damaged switch reads %+v, want off", it)
	}
	if it := items[jevshadowsettings.KeyCandidateVersion]; it.Value != "" {
		t.Errorf("damaged candidate reads %+v, want none", it)
	}
	srv.call(t, "settings_set", map[string]any{"key": "jev.shadow.enabled", "value": "true"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
}
