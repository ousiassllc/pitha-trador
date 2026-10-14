package opsflow_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// issue #759: the meta-labeling model, calibration, nightly retrain time and
// minimum EV are edited on the Settings screen, one key per row.
func TestSaveOps_MetaLabelSettingsAreEditable(t *testing.T) {
	engine, store := opsRouter()

	for key, tc := range map[string]struct{ raw, stored string }{
		metalabelsettings.KeyModel:       {"logistic_regression", `"logistic_regression"`},
		metalabelsettings.KeyCalibration: {"isotonic", `"isotonic"`},
		metalabelsettings.KeyRetrainTime: {"19:30", `"19:30"`},
		metalabelsettings.KeyMinEVBps:    {"3.5", `3.5`},
	} {
		rec := opsRequest(engine, http.MethodPost, key, url.Values{"value": {tc.raw}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, body=%s", key, rec.Code, rec.Body.String())
		}
		if store.rows[key] != tc.stored {
			t.Errorf("stored %s = %q, want %q", key, store.rows[key], tc.stored)
		}
		if !strings.Contains(rec.Body.String(), `id="setting-group-status-metalabel"`) {
			t.Errorf("POST %s: response lacks the group status; body=%s", key, rec.Body.String())
		}
	}
}

func TestSaveOps_MetaLabelRejectsInvalidValues(t *testing.T) {
	engine, store := opsRouter()

	for name, tc := range map[string]struct{ key, raw string }{
		"unknown model":       {metalabelsettings.KeyModel, "lightgbm"},
		"unknown calibration": {metalabelsettings.KeyCalibration, "beta"},
		"malformed time":      {metalabelsettings.KeyRetrainTime, "25:00"},
		"time during session": {metalabelsettings.KeyRetrainTime, "11:00"},
		"negative min EV":     {metalabelsettings.KeyMinEVBps, "-1"},
		"min EV text":         {metalabelsettings.KeyMinEVBps, "abc"},
	} {
		rec := opsRequest(engine, http.MethodPost, tc.key, url.Values{"value": {tc.raw}}, true)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: POST %s=%q status = %d, want 400", name, tc.key, tc.raw, rec.Code)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}
}
