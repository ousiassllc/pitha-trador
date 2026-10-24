package opsflow_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

func modeRouter(mode metalabelsettings.Mode) (*gin.Engine, *memRuntimeStore) {
	gin.SetMode(gin.TestMode)
	store := &memRuntimeStore{rows: map[string]string{}}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).
		WithOperationalSettings(opsettings.New(store, opsStrategy(), "/data/logs").WithTradingMode(mode))
	engine := settingsRouter(h)
	engine.POST("/ops-settings/:key", h.SaveOps)
	engine.DELETE("/ops-settings/:key", h.ResetOps)
	return engine, store
}

// issue #798: the bootstrap switch is on the 取引ルール page with its note, and in
// Paper it can be turned on and reset.
func TestPaperBootstrap_PaperCanTurnItOn(t *testing.T) {
	engine, store := modeRouter(metalabelsettings.ModePaper)

	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, settingspages.BasePath+"/"+settingspages.PageOperations, nil))
	for _, want := range []string{"modal-paper-bootstrap", `setting-field-row-` + paperbootstrapsettings.Key, "Live では保存値があっても必ず無効"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the 取引ルール page lacks %q", want)
		}
	}

	rec := opsRequest(engine, http.MethodPost, paperbootstrapsettings.Key, url.Values{"value": {"1"}}, true)
	if rec.Code != http.StatusOK || store.rows[paperbootstrapsettings.Key] != "1" {
		t.Fatalf("POST 1: status = %d, stored = %q, body=%s", rec.Code, store.rows[paperbootstrapsettings.Key], rec.Body.String())
	}
	if rec := opsRequest(engine, http.MethodDelete, paperbootstrapsettings.Key, nil, true); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: status = %d", rec.Code)
	}
	if _, ok := store.rows[paperbootstrapsettings.Key]; ok {
		t.Error("reset left the row")
	}
}

// HARD RULE: outside Paper the screen refuses to turn it on (400 with the
// reason, nothing stored) and shows a stored "on" as inactive.
func TestPaperBootstrap_LiveRefusesToTurnItOn(t *testing.T) {
	engine, store := modeRouter(metalabelsettings.ModeLive)

	rec := opsRequest(engine, http.MethodPost, paperbootstrapsettings.Key, url.Values{"value": {"1"}}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST 1 in Live: status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Paper 限定") {
		t.Errorf("the 400 does not give the reason: %s", rec.Body.String())
	}
	if len(store.rows) != 0 {
		t.Errorf("the refused value was stored: %v", store.rows)
	}

	store.rows[paperbootstrapsettings.Key] = "1"
	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, settingspages.BasePath+"/"+settingspages.PageOperations, nil))
	if !strings.Contains(page.Body.String(), "Live では無効です") {
		t.Errorf("a stored on is not shown as inactive in Live")
	}
}
