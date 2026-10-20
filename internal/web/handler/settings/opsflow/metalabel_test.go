package opsflow_test

import (
	"net/http"
	"net/http/httptest"
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
		metalabelsettings.KeyLLMCutoff:   {"2025-09-30", `"2025-09-30"`},
		// issue #776: the cutoff's provenance is recorded as runtime settings too.
		metalabelsettings.KeyLLMCutoffModel:       {"jev-x", `"jev-x"`},
		metalabelsettings.KeyLLMCutoffConfirmedOn: {"2026-10-17", `"2026-10-17"`},
		metalabelsettings.KeyLLMCutoffSource:      {"vendor docs", `"vendor docs"`},
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
		"malformed cutoff":    {metalabelsettings.KeyLLMCutoff, "2025/09/30"},
		"malformed confirmed": {metalabelsettings.KeyLLMCutoffConfirmedOn, "yesterday"},
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

// issue #776: the LLM cutoff row warns while it is the placeholder and the
// warning disappears once a real cutoff with its Jev model is stored.
func TestSettingsPage_WarnsWhileTheLLMCutoffIsAPlaceholder(t *testing.T) {
	warning := `data-testid="warning-` + metalabelsettings.KeyLLMCutoff + `"`
	engine, store := opsRouter()
	page := func() string {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/results", nil))
		return rec.Body.String()
	}

	if body := page(); !strings.Contains(body, warning) || !strings.Contains(body, "仮置き") {
		t.Fatal("the placeholder cutoff shows no warning on the Settings page")
	}
	store.rows[metalabelsettings.KeyLLMCutoff] = `"2026-09-01"`
	store.rows[metalabelsettings.KeyLLMCutoffModel] = `"jev-x"`
	if strings.Contains(page(), warning) {
		t.Error("a confirmed cutoff still shows the warning")
	}
}

// issue #809: the meta-labeling group shows the cutoff status (placeholder /
// unverified / confirmed) with the how-to of confirming the official date.
// Each save refreshes the panel out-of-band, so entering the date, then the
// Jev model, moves the status on the screen from 仮置き through 未確認 to 確認済み.
func TestSettingsPage_ShowsTheCutoffStatusAndHelp(t *testing.T) {
	engine, _ := opsRouter()
	status := `data-testid="setting-group-derived-metalabel-cutoff-status"`
	statusLine := func(body string) string {
		_, rest, ok := strings.Cut(body, status)
		if !ok {
			t.Fatalf("the status line is missing; body=%s", body)
		}
		line, _, _ := strings.Cut(rest, "</li>")
		return line
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings/results", nil))
	page := rec.Body.String()
	if !strings.Contains(statusLine(page), "仮置き（placeholder）") {
		t.Errorf("the default status is not placeholder: %s", statusLine(page))
	}
	for _, want := range []string{"公式の日付の確認手順", metalabelsettings.KeyLLMCutoffSource, "knowledge cutoff", "YYYY-MM-DD"} {
		if !strings.Contains(page, want) {
			t.Errorf("the help lacks %q", want)
		}
	}

	for _, step := range []struct{ key, value, want string }{
		{metalabelsettings.KeyLLMCutoff, "2026-03-31", "未確認（unverified）"},
		{metalabelsettings.KeyLLMCutoffSource, "https://example.com/model-card", "未確認（unverified）"},
		{metalabelsettings.KeyLLMCutoffModel, "jev-2026-03", "確認済み（confirmed）"},
	} {
		rec := opsRequest(engine, http.MethodPost, step.key, url.Values{"value": {step.value}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, body=%s", step.key, rec.Code, rec.Body.String())
		}
		if got := statusLine(rec.Body.String()); !strings.Contains(got, step.want) {
			t.Errorf("after %s the refreshed panel says %s, want %s", step.key, got, step.want)
		}
	}
}
