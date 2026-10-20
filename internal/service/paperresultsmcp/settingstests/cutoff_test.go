package settingstests

import (
	"net/http"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
)

// Issue #809: the operator replaces the placeholder LLM cutoff and records its
// provenance with settings_set alone, and settings_get shows the status code
// after each step: placeholder -> unverified (date only) -> confirmed (model
// recorded). The write response carries the status too.
func TestCutoffCanBeConfirmedThroughTheMCPAlone(t *testing.T) {
	srv, _ := settingsServer(t, nil)

	if got := getState(srv, t).LLMCutoffStatus; got != metalabelsettings.CutoffPlaceholder {
		t.Fatalf("fresh status = %q, want %q", got, metalabelsettings.CutoffPlaceholder)
	}
	if w := getState(srv, t).item(t, metalabelsettings.KeyLLMCutoff).Warning; w == "" {
		t.Error("the placeholder cutoff carries no warning in settings_get")
	}

	steps := []struct {
		key, value, wantStatus string
	}{
		{metalabelsettings.KeyLLMCutoff, "2026-03-31", metalabelsettings.CutoffUnverified}, // date alone: not confirmed
		{metalabelsettings.KeyLLMCutoffConfirmedOn, "2026-10-20", metalabelsettings.CutoffUnverified},
		{metalabelsettings.KeyLLMCutoffSource, "https://example.com/model-card", metalabelsettings.CutoffUnverified},
		{metalabelsettings.KeyLLMCutoffModel, "jev-2026-03", metalabelsettings.CutoffConfirmed},
	}
	for _, step := range steps {
		var out struct {
			Value  string `json:"value"`
			Status string `json:"llm_cutoff_status"`
		}
		set(srv, t, step.key, step.value).data(t, "settings_set", &out)
		if out.Value != step.value || out.Status != step.wantStatus {
			t.Fatalf("settings_set %s: %+v, want value %q status %q", step.key, out, step.value, step.wantStatus)
		}
		if got := getState(srv, t).LLMCutoffStatus; got != step.wantStatus {
			t.Fatalf("settings_get after %s: status %q, want %q", step.key, got, step.wantStatus)
		}
	}

	st := getState(srv, t)
	for _, key := range []string{metalabelsettings.KeyLLMCutoff, metalabelsettings.KeyLLMCutoffModel, metalabelsettings.KeyLLMCutoffConfirmedOn, metalabelsettings.KeyLLMCutoffSource} {
		if it := st.item(t, key); it.Source != "stored" {
			t.Errorf("%s source = %q, want stored", key, it.Source)
		}
	}
	if w := st.item(t, metalabelsettings.KeyLLMCutoff).Warning; w != "" {
		t.Errorf("the confirmed cutoff still warns: %q", w)
	}
}

// Writing a non-cutoff key does not add the status to the response, and a
// rejected cutoff value leaves the status as it was.
func TestCutoffStatusOnlyOnCutoffKeysAndNotChangedByRejectedValues(t *testing.T) {
	srv, _ := settingsServer(t, nil)
	var out map[string]any
	set(srv, t, "risk.paper.max_trade_loss_pct", 2).data(t, "settings_set", &out)
	if _, ok := out["llm_cutoff_status"]; ok {
		t.Error("a non-cutoff write returned llm_cutoff_status")
	}
	set(srv, t, metalabelsettings.KeyLLMCutoff, "not-a-date").wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	if got := getState(srv, t).LLMCutoffStatus; got != metalabelsettings.CutoffPlaceholder {
		t.Errorf("status after a rejected value = %q, want %q", got, metalabelsettings.CutoffPlaceholder)
	}
}
