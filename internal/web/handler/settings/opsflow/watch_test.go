package opsflow_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

// issue #1055: the watch size and the observation settings are on the 運用設定
// page with the reason for the cap, the default is today's 45, a value above
// the kabu stream cap is refused (422) and the stored row stays.
func TestWatchSettings_OnTheOperationsPage(t *testing.T) {
	engine, store := modeRouter(metalabelsettings.ModePaper)

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, settingspages.BasePath+"/"+settingspages.PageOperations, nil))
	for _, want := range []string{
		"modal-ranking-watch", `setting-field-row-` + watchsettings.KeyMaxWatched, `setting-field-row-` + watchsettings.KeyObservationEnabled,
		`setting-field-row-` + watchsettings.KeyObservationDailyCostCapUSD,
		"API 登録枠 50 件のうち 5 件", "429 や 4001006", "一切乗せません", "通常の Scout・Trader は止まりません",
	} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the 運用設定 page lacks %q", want)
		}
	}

	if rec := opsRequest(engine, http.MethodPost, watchsettings.KeyMaxWatched, url.Values{"value": {"30"}}, true); rec.Code != http.StatusOK || store.rows[watchsettings.KeyMaxWatched] != "30" {
		t.Fatalf("POST 30: status = %d, stored = %q, body=%s", rec.Code, store.rows[watchsettings.KeyMaxWatched], rec.Body.String())
	}
	rec := opsRequest(engine, http.MethodPost, watchsettings.KeyMaxWatched, url.Values{"value": {"46"}}, true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST 46: status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "最大 45 件") {
		t.Errorf("the refusal does not show its reason: %s", rec.Body.String())
	}
	if store.rows[watchsettings.KeyMaxWatched] != "30" {
		t.Errorf("the refused value changed the row: %q", store.rows[watchsettings.KeyMaxWatched])
	}
	if rec := opsRequest(engine, http.MethodDelete, watchsettings.KeyMaxWatched, nil, true); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: status = %d", rec.Code)
	}
	if _, ok := store.rows[watchsettings.KeyMaxWatched]; ok {
		t.Error("reset left the row")
	}
}
