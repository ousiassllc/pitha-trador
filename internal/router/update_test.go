package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
)

type fakeUpdateController struct{ checks int }

func (f *fakeUpdateController) Status() updater.Status {
	return updater.Status{Available: true, Version: "v9.9.9", Blocked: true}
}

func (f *fakeUpdateController) CheckForUpdate(context.Context) error {
	f.checks++
	return nil
}

func TestNew_UpdateRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller := &fakeUpdateController{}
	engine := router.New(router.WithUpdateController(controller))

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/system/update-status", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v9.9.9") {
		t.Fatalf("GET /system/update-status = %d %q, want 200 with the available version", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(http.MethodPost, "/system/update-check", nil)))
	if rec.Code != http.StatusOK || controller.checks != 1 {
		t.Fatalf("POST /system/update-check = %d with %d checks, want 200 with 1", rec.Code, controller.checks)
	}
}

func TestNew_UpdateCheckRouteIs404WithoutUpdater(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, authorize(t, engine, httptest.NewRequest(http.MethodPost, "/system/update-check", nil)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cmd/server has no updater)", rec.Code)
	}
}

func TestNew_SettingsAndHeaderEmbedUpdateSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	router.New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	for _, want := range []string{`id="update-banner"`, `id="update-panel"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Settings page missing %s; body=%s", want, body)
		}
	}
}
