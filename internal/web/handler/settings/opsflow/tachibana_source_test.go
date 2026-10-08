package opsflow_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

func TestSettingsPage_ShowsTachibanaSourceCards(t *testing.T) {
	engine, _ := opsRouter()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	for _, id := range []string{"tachibana-source", "tachibana-nightly", "tachibana-screen", "tachibana-intraday"} {
		if !strings.Contains(body, `data-testid="settings-card-`+id+`"`) {
			t.Errorf("page has no card %q", id)
		}
	}
	for _, key := range tachibanasource.TachibanaSourceSettingKeys() {
		if n := strings.Count(body, `data-testid="setting-field-row-`+key+`"`); n != 1 {
			t.Errorf("page renders %d rows for %q, want exactly 1", n, key)
		}
	}
}

func TestSaveOps_TachibanaSourceIsStoredNormalizedAndBadValuesAre400(t *testing.T) {
	engine, store := opsRouter()
	rec := opsRequest(engine, http.MethodPost, tachibanasource.KeyTachibanaManualSymbols, url.Values{"value": {"7203 6758"}}, true)
	if rec.Code != http.StatusOK || store.rows[tachibanasource.KeyTachibanaManualSymbols] != `"7203,6758"` {
		t.Fatalf("status = %d, store = %v", rec.Code, store.rows)
	}
	if rec := opsRequest(engine, http.MethodPost, tachibanasource.KeyTachibanaNightlyRunTime, url.Values{"value": {"12:00"}}, true); rec.Code != http.StatusBadRequest {
		t.Errorf("a daytime nightly run time status = %d, want 400", rec.Code)
	}
	if rec := opsRequest(engine, http.MethodPost, tachibanasource.KeyTachibanaEventMaxConnects, url.Values{"value": {"0"}}, true); rec.Code != http.StatusBadRequest {
		t.Errorf("event budget 0 status = %d, want 400", rec.Code)
	}
	if rec := opsRequest(engine, http.MethodPost, tachibanasource.KeyTachibanaNightlyRatePerSecond, url.Values{"value": {"2.5"}}, true); rec.Code != http.StatusOK || store.rows[tachibanasource.KeyTachibanaNightlyRatePerSecond] != `2.5` {
		t.Errorf("rate status = %d, store = %v", rec.Code, store.rows)
	}
	if rec := opsRequest(engine, http.MethodDelete, tachibanasource.KeyTachibanaManualSymbols, nil, true); rec.Code != http.StatusOK || store.rows[tachibanasource.KeyTachibanaManualSymbols] != "" {
		t.Errorf("reset status = %d, store = %v, want the row removed", rec.Code, store.rows)
	}
}
