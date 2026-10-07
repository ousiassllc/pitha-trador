package opsroutes_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// fakeOps is a settings.OperationalSettings recording the last saved key.
type fakeOps struct{ saved string }

func (f *fakeOps) Get(_ context.Context, key string) (opsettings.Value, error) {
	return opsettings.Value{Key: key}, nil
}
func (f *fakeOps) Save(_ context.Context, key, _ string) error { f.saved = key; return nil }
func (f *fakeOps) Reset(context.Context, string) error         { return nil }

// postOps loads a page from engine for a session + CSRF token, then POSTs a
// backup directory to `/ops-settings/:key`.
func postOps(t *testing.T, engine *gin.Engine) int {
	t.Helper()
	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/settings", nil))
	match := csrfMeta.FindStringSubmatch(page.Body.String())
	if cookies := page.Result().Cookies(); len(cookies) != 1 || match == nil {
		t.Fatalf("GET /settings gave cookies=%v csrf meta=%v", cookies, match)
	}
	req := httptest.NewRequest(http.MethodPost, "/ops-settings/"+config.KeyBackupDir, strings.NewReader("value=/mnt/b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set(middleware.CSRFHeader, match[1])
	req.AddCookie(page.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code
}

// Issue #708: the 運用設定 routes exist only with WithOperationalSettings.
func TestNew_OperationalSettingsRoutesNeedWithOperationalSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if got := postOps(t, router.New()); got != http.StatusNotFound {
		t.Fatalf("POST without the option = %d, want 404", got)
	}
	ops := &fakeOps{}
	if got := postOps(t, router.New(router.WithOperationalSettings(ops))); got != http.StatusOK || ops.saved != config.KeyBackupDir {
		t.Fatalf("POST with the option = %d, saved = %q, want 200 and %q", got, ops.saved, config.KeyBackupDir)
	}
}
