package settingstests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// Issue #1220: the お試し記録's six operator keys are on settings_set / settings_get;
// its runner-state keys are not.

const (
	armOR    = `{"id":"or-a","provider":"openrouter","model":"vendor/model-a","prompt_version":"trader-v3","daily_cost_cap_usd":0.04}`
	armCodex = `{"id":"cx","provider":"codex_chatgpt","model":"gpt-5","prompt_version":"trader-v3"}`
)

func arms(items ...string) string { return "[" + strings.Join(items, ",") + "]" }

var trialKeys = jevtrialsettings.OperatorKeys()

// storedTrial returns the trial keys' stored values (absent keys omitted).
func storedTrial(t *testing.T, w *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, key := range jevtrialsettings.Keys() {
		if v, ok := storedValue(t, w, key); ok {
			out[key] = v
		}
	}
	return out
}

// settings_get lists the six keys with the defaults, the unit, a description and the
// accepted range, and nothing else of the trial.
func TestTrialSettingsGetListsTheSixKeysWithDefaults(t *testing.T) {
	srv, _ := settingsServer(t, nil)
	var st struct {
		Items []struct {
			Key         string `json:"key"`
			Value       string `json:"value"`
			Default     string `json:"default"`
			Source      string `json:"source"`
			Unit        string `json:"unit"`
			Description string `json:"description"`
			Constraint  string `json:"constraint"`
		} `json:"items"`
	}
	srv.call(t, "settings_get", map[string]any{}).data(t, "settings_get", &st)
	want := map[string]struct{ value, unit string }{
		"jev.trial.enabled":               {"false", "text"},
		"jev.trial.arms":                  {"[]", "text"},
		"jev.trial.daily_cost_cap_usd":    {"0.1", "text"},
		"jev.trial.codex.enabled":         {"false", "text"},
		"jev.trial.codex.daily_call_cap":  {"30", "count"},
		"jev.trial.codex.daily_token_cap": {"500000", "count"},
	}
	got := 0
	for _, it := range st.Items {
		if !strings.HasPrefix(it.Key, "jev.trial.") {
			continue
		}
		got++
		w, ok := want[it.Key]
		if !ok {
			t.Errorf("settings_get lists %s", it.Key)
			continue
		}
		if it.Value != w.value || it.Default != w.value || it.Source != "default" || it.Unit != w.unit || it.Description == "" || it.Constraint == "" {
			t.Errorf("%s = %+v, want default %q unit %q with a description and a constraint", it.Key, it, w.value, w.unit)
		}
	}
	if got != 6 || len(st.Items) != 35 {
		t.Errorf("trial keys listed = %d, items = %d, want 6 and 35", got, len(st.Items))
	}
}

// Every key is written through settings_set, stored in the form the runner and the
// Settings screen read, and what settings_get shows is what the screen's service
// (jevtrial.Service.State) and the runner's loader (jevtrialsettings.Load) see.
func TestTrialSettingsSetStoresWhatTheSettingsScreenReads(t *testing.T) {
	srv, w := settingsServer(t, nil)
	for _, c := range []struct {
		key   string
		value any
	}{
		{"jev.trial.codex.enabled", "true"},
		{"jev.trial.arms", arms(armOR, armCodex)},
		{"jev.trial.daily_cost_cap_usd", 0.25},
		{"jev.trial.codex.daily_call_cap", 12},
		{"jev.trial.codex.daily_token_cap", 250000},
		{"jev.trial.enabled", 1}, // 1 / 0 are accepted as well as true / false
	} {
		var out struct {
			Key    string  `json:"key"`
			Value  string  `json:"value"`
			Before *string `json:"before"`
		}
		set(srv, t, c.key, c.value).data(t, "settings_set", &out)
		if out.Key != c.key || out.Before != nil {
			t.Errorf("%s: response %+v", c.key, out)
		}
	}
	stored := storedTrial(t, w)
	if stored["jev.trial.enabled"] != "1" || stored["jev.trial.codex.enabled"] != "1" || stored["jev.trial.daily_cost_cap_usd"] != "0.25" ||
		stored["jev.trial.codex.daily_call_cap"] != "12" || stored["jev.trial.codex.daily_token_cap"] != "250000" {
		t.Errorf("stored = %v", stored)
	}
	parsed, err := jevtrialsettings.ParseArms(stored["jev.trial.arms"])
	if err != nil || len(parsed) != 2 || parsed[0].ID != "or-a" || parsed[0].DailyCostCapUSD != 0.04 || parsed[1].Provider != jevtrialsettings.ProviderCodexChatGPT {
		t.Fatalf("stored arms = %q (%v)", stored["jev.trial.arms"], err)
	}

	// The Settings screen's service reads the same rows.
	state, err := jevtrial.New(w).State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.CapUSD != 0.25 || state.CapIsDefault || len(state.Arms) != 2 || state.Arms[0].Arm.ID != "or-a" ||
		!state.Codex.Enabled || state.Codex.CallCap != 12 || state.Codex.TokenCap != 250000 {
		t.Errorf("screen state = %+v", state)
	}
	// And settings_get reads them back as the same values.
	st := getState(srv, t)
	for key, want := range map[string]string{
		"jev.trial.enabled": "true", "jev.trial.daily_cost_cap_usd": "0.25", "jev.trial.codex.enabled": "true",
		"jev.trial.codex.daily_call_cap": "12", "jev.trial.codex.daily_token_cap": "250000",
	} {
		if it := st.item(t, key); it.Value != want || it.Source != "stored" {
			t.Errorf("settings_get %s = %+v, want %q stored", key, it, want)
		}
	}
	var back []jevtrialsettings.Arm
	if err := json.Unmarshal([]byte(st.item(t, "jev.trial.arms").Value), &back); err != nil || !reflect.DeepEqual(back, parsed) {
		t.Errorf("settings_get arms = %q (%v)", st.item(t, "jev.trial.arms").Value, err)
	}
}

// The arms are edited by writing the whole array: removing one and writing none.
func TestTrialSettingsSetArmsReplacesTheWholeArray(t *testing.T) {
	srv, w := settingsServer(t, nil)
	set(srv, t, "jev.trial.arms", arms(armOR, `{"id":"or-b","provider":"openrouter","model":"vendor/model-b","prompt_version":"trader-v3"}`)).data(t, "settings_set", &struct{}{})
	set(srv, t, "jev.trial.arms", arms(armOR)).data(t, "settings_set", &struct{}{})
	if got, _ := jevtrialsettings.ParseArms(storedTrial(t, w)["jev.trial.arms"]); len(got) != 1 || got[0].ID != "or-a" {
		t.Errorf("after removing an arm: %+v", got)
	}
	set(srv, t, "jev.trial.arms", "[]").data(t, "settings_set", &struct{}{})
	if v := storedTrial(t, w)["jev.trial.arms"]; v != "[]" {
		t.Errorf("no arms stored as %q", v)
	}
	// The trial may be turned on with no arm, as on the screen: the runner then calls nothing.
	set(srv, t, "jev.trial.enabled", true).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument) // a JSON boolean is not a value
	set(srv, t, "jev.trial.enabled", "true").data(t, "settings_set", &struct{}{})
}

// A value the Settings screen would refuse is refused with invalid_value and the reason,
// and nothing is stored or audited.
func TestTrialSettingsSetRefusesWhatTheSettingsScreenRefuses(t *testing.T) {
	srv, w := settingsServer(t, nil)
	nine := make([]string, jevtrialsettings.MaxArms+1)
	for i := range nine {
		nine[i] = strings.Replace(armOR, `"or-a"`, `"a`+string(rune('0'+i))+`"`, 1)
	}
	for _, c := range []struct {
		name  string
		key   string
		value any
		want  string // part of the reason
	}{
		{"duplicate id", "jev.trial.arms", arms(armOR, armOR), "重複"},
		{"unregistered prompt", "jev.trial.arms", arms(strings.Replace(armOR, "trader-v3", "trader-v999", 1)), "登録されていません"},
		{"nine arms", "jev.trial.arms", "[" + strings.Join(nine, ",") + "]", "最大"},
		{"not JSON", "jev.trial.arms", "not json", "JSON"},
		{"an object, not an array", "jev.trial.arms", armOR, "JSON"},
		{"unknown field", "jev.trial.arms", arms(strings.Replace(armOR, `"model"`, `"modle"`, 1)), "JSON"},
		{"provider_only on custom", "jev.trial.arms", arms(`{"id":"c","provider":"custom","model":"m","prompt_version":"trader-v3","provider_only":["x"]}`), "OpenRouter"},
		{"ZDR on custom", "jev.trial.arms", arms(`{"id":"c","provider":"custom","model":"m","prompt_version":"trader-v3","data_collection":"deny"}`), "OpenRouter"},
		{"codex arm with an endpoint", "jev.trial.arms", arms(`{"id":"cx","provider":"codex_chatgpt","model":"m","prompt_version":"trader-v3","endpoint":"/alpha/decisions"}`), "codex_chatgpt"},
		{"codex arm with a USD cap", "jev.trial.arms", arms(`{"id":"cx","provider":"codex_chatgpt","model":"m","prompt_version":"trader-v3","daily_cost_cap_usd":0.01}`), "codex_chatgpt"},
		{"codex arm while codex is off", "jev.trial.arms", arms(armCodex), "ChatGPT サブスク"},
		{"cap zero", "jev.trial.daily_cost_cap_usd", 0, "USD"},
		{"cap negative", "jev.trial.daily_cost_cap_usd", -1, "USD"},
		{"cap above the maximum", "jev.trial.daily_cost_cap_usd", 100.01, "USD"},
		{"cap not a number", "jev.trial.daily_cost_cap_usd", "abc", "数値"},
		{"cap NaN", "jev.trial.daily_cost_cap_usd", "NaN", "USD"},
		{"cap infinite", "jev.trial.daily_cost_cap_usd", "Inf", "USD"},
		{"switch not boolean", "jev.trial.enabled", "maybe", "真偽"},
		{"empty switch", "jev.trial.enabled", "  ", ""},
		{"codex switch not boolean", "jev.trial.codex.enabled", "2", "真偽"},
		{"codex call cap zero", "jev.trial.codex.daily_call_cap", 0, "1〜"},
		{"codex call cap too large", "jev.trial.codex.daily_call_cap", jevtrialsettings.MaxCodexDailyCallCap + 1, "1〜"},
		{"codex call cap fractional", "jev.trial.codex.daily_call_cap", 1.5, "整数"},
		{"codex token cap zero", "jev.trial.codex.daily_token_cap", 0, "1〜"},
		{"codex token cap too large", "jev.trial.codex.daily_token_cap", jevtrialsettings.MaxCodexDailyTokenCap + 1, "1〜"},
	} {
		r := set(srv, t, c.key, c.value)
		if c.name == "empty switch" {
			r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
			continue
		}
		r.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
		if detail := r.errorBody(t).Error.Data["detail"]; !strings.Contains(detail, c.want) {
			t.Errorf("%s: reason %q lacks %q", c.name, detail, c.want)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM runtime_settings`) + count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Fatalf("refused writes left %d rows behind", n)
	}
}

// The Codex switch cannot be turned off while a codex_chatgpt arm is saved, and a codex arm
// can be saved once the switch is on; there is no way to remove a cap.
func TestTrialSettingsSetKeepsCodexArmsAndSwitchConsistent(t *testing.T) {
	srv, w := settingsServer(t, nil)
	set(srv, t, "jev.trial.codex.enabled", "true").data(t, "settings_set", &struct{}{})
	set(srv, t, "jev.trial.arms", arms(armOR, armCodex)).data(t, "settings_set", &struct{}{})
	set(srv, t, "jev.trial.codex.enabled", "false").wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if v := storedTrial(t, w)["jev.trial.codex.enabled"]; v != "1" {
		t.Errorf("a refused switch change stored %q", v)
	}
	set(srv, t, "jev.trial.arms", arms(armOR)).data(t, "settings_set", &struct{}{})
	set(srv, t, "jev.trial.codex.enabled", "false").data(t, "settings_set", &struct{}{})
	if v := storedTrial(t, w)["jev.trial.codex.enabled"]; v != "0" {
		t.Errorf("codex switch = %q, want 0", v)
	}
}
