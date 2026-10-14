package opsflow_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
)

// issue #755: every regime parameter is edited on the Settings screen, one key
// per row.
func TestSaveOps_RegimeParametersAreEditable(t *testing.T) {
	engine, store := opsRouter()

	for key, raw := range map[string]string{
		regimesettings.KeyABEnabled:         "1",
		regimesettings.KeyWindowMinutes:     "45",
		regimesettings.KeyAutocorrThreshold: "0.3",
		regimesettings.KeyMinVolatilityBps:  "1.5",
		regimesettings.KeyVWAPZThreshold:    "2.5",
	} {
		rec := opsRequest(engine, http.MethodPost, key, url.Values{"value": {raw}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, body=%s", key, rec.Code, rec.Body.String())
		}
		if store.rows[key] != raw {
			t.Errorf("stored %s = %q, want %q", key, store.rows[key], raw)
		}
		if !strings.Contains(rec.Body.String(), `id="setting-group-status-regime"`) {
			t.Errorf("POST %s: response lacks the group status; body=%s", key, rec.Body.String())
		}
	}
}

func TestSaveOps_RegimeRejectsInvalidValues(t *testing.T) {
	engine, store := opsRouter()

	for name, tc := range map[string]struct{ key, raw string }{
		"switch not 0 or 1":    {regimesettings.KeyABEnabled, "2"},
		"window below minimum": {regimesettings.KeyWindowMinutes, "1"},
		"autocorr one":         {regimesettings.KeyAutocorrThreshold, "1"},
		"z zero":               {regimesettings.KeyVWAPZThreshold, "0"},
		"text":                 {regimesettings.KeyMinVolatilityBps, "abc"},
	} {
		rec := opsRequest(engine, http.MethodPost, tc.key, url.Values{"value": {tc.raw}}, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: POST %s=%q status = %d, want 400", name, tc.key, tc.raw, rec.Code)
		}
	}
	if len(store.rows) != 0 {
		t.Errorf("rejected values were stored: %v", store.rows)
	}
}
