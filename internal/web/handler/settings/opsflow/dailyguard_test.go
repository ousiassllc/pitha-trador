package opsflow_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

// issue #753: every daily entry-halt threshold is edited on the Settings
// screen, one key per row, and 0 disables the rule.
func TestSaveOps_DailyGuardThresholdsAreEditable(t *testing.T) {
	engine, store := opsRouter()

	for key, tc := range map[string]struct{ raw, stored string }{
		dailyguardsettings.KeyLossLimitJPY:          {"50000", "50000"},
		dailyguardsettings.KeyProfitLockTriggerJPY:  {"30000.5", "30000.5"},
		dailyguardsettings.KeyProfitLockGivebackPct: {"30", "30"},
		dailyguardsettings.KeyMaxTrades:             {"8", "8"},
	} {
		rec := opsRequest(engine, http.MethodPost, key, url.Values{"value": {tc.raw}}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: status = %d, body=%s", key, rec.Code, rec.Body.String())
		}
		if store.rows[key] != tc.stored {
			t.Errorf("stored %s = %q, want %q", key, store.rows[key], tc.stored)
		}
		if !strings.Contains(rec.Body.String(), `id="setting-group-status-daily-guard"`) {
			t.Errorf("POST %s: response lacks the group status; body=%s", key, rec.Body.String())
		}
	}

	// 0 stores as a value that disables the rule.
	if rec := opsRequest(engine, http.MethodPost, dailyguardsettings.KeyLossLimitJPY, url.Values{"value": {"0"}}, true); rec.Code != http.StatusOK {
		t.Fatalf("POST 0: status = %d", rec.Code)
	}
	if store.rows[dailyguardsettings.KeyLossLimitJPY] != "0" {
		t.Errorf("stored 0 = %q", store.rows[dailyguardsettings.KeyLossLimitJPY])
	}
}

func TestSaveOps_DailyGuardRejectsInvalidValues(t *testing.T) {
	engine, store := opsRouter()

	for name, tc := range map[string]struct{ key, raw string }{
		"negative loss limit":       {dailyguardsettings.KeyLossLimitJPY, "-1"},
		"text":                      {dailyguardsettings.KeyLossLimitJPY, "abc"},
		"giveback above 100":        {dailyguardsettings.KeyProfitLockGivebackPct, "100.5"},
		"fractional trade count":    {dailyguardsettings.KeyMaxTrades, "2.5"},
		"negative trade count":      {dailyguardsettings.KeyMaxTrades, "-3"},
		"negative profit trigger":   {dailyguardsettings.KeyProfitLockTriggerJPY, "-100"},
		"infinite loss limit":       {dailyguardsettings.KeyLossLimitJPY, "Inf"},
		"not-a-number profit value": {dailyguardsettings.KeyProfitLockTriggerJPY, "NaN"},
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

func TestResetOps_DailyGuardResetDisablesTheRule(t *testing.T) {
	engine, store := opsRouter()
	store.rows[dailyguardsettings.KeyMaxTrades] = "5"

	rec := opsRequest(engine, http.MethodDelete, dailyguardsettings.KeyMaxTrades, nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := store.rows[dailyguardsettings.KeyMaxTrades]; ok {
		t.Fatal("Reset left the row")
	}
	if !strings.Contains(rec.Body.String(), "0（無効）") {
		t.Errorf("response does not show the disabled default; body=%s", rec.Body.String())
	}
}
