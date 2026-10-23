package opsflow_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/sleepguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// issue #863: the sleep guard switch is on the システム page (default off), can be
// turned on and reset, and refuses anything but 0 / 1.
func TestSleepGuard_SwitchOnTheSystemPage(t *testing.T) {
	engine, store := modeRouter(metalabelsettings.ModePaper)

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, settingspages.BasePath+"/"+settingspages.PageMaintenance, nil))
	for _, want := range []string{"modal-sleep-guard", `setting-field-row-` + sleepguardsettings.Key, "既定は 0（無効）", "Windows のみ"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the システム page lacks %q", want)
		}
	}

	rec := opsRequest(engine, http.MethodPost, sleepguardsettings.Key, url.Values{"value": {"1"}}, true)
	if rec.Code != http.StatusOK || store.rows[sleepguardsettings.Key] != "1" {
		t.Fatalf("POST 1: status = %d, stored = %q, body=%s", rec.Code, store.rows[sleepguardsettings.Key], rec.Body.String())
	}
	if rec := opsRequest(engine, http.MethodPost, sleepguardsettings.Key, url.Values{"value": {"2"}}, true); rec.Code != http.StatusBadRequest {
		t.Errorf("POST 2: status = %d, want 400", rec.Code)
	}
	if store.rows[sleepguardsettings.Key] != "1" {
		t.Errorf("the refused value changed the row: %q", store.rows[sleepguardsettings.Key])
	}
	if rec := opsRequest(engine, http.MethodDelete, sleepguardsettings.Key, nil, true); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: status = %d", rec.Code)
	}
	if _, ok := store.rows[sleepguardsettings.Key]; ok {
		t.Error("reset left the row")
	}
}
