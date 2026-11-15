// Package migrationroute holds the router-level tests of the PC migration
// bundle endpoints (issue #1075, FR-MIGRATE-*): session / CSRF protection and
// the export -> preview -> confirm flow over real services on a temp SQLite
// database. The handler's error mapping is covered in
// internal/web/handler/migration.
package migrationroute_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/bundletest"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

var csrfMeta = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

type env struct {
	engine *gin.Engine
	cookie *http.Cookie
	csrf   string
}

func newEnv(t *testing.T, opts ...router.Option) env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := router.New(opts...)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	cookies, match := rec.Result().Cookies(), csrfMeta.FindStringSubmatch(rec.Body.String())
	if len(cookies) != 1 || match == nil {
		t.Fatalf("GET /setup: cookies %v, csrf %v", cookies, match)
	}
	return env{engine, cookies[0], match[1]}
}

// do sends a request; withCookie/withCSRF choose what a browser would attach.
func (e env) do(method, path string, body io.Reader, withCookie, withCSRF bool, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if withCookie {
		req.AddCookie(e.cookie)
	}
	if withCSRF {
		req.Header.Set(middleware.CSRFHeader, e.csrf)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

func (e env) session(method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	return e.do(method, path, body, true, true, contentType)
}

// wired returns a router over a populated "old PC" database serving the
// migration endpoints, plus the database of the "new PC" the import flow
// belongs to.
func wired(t *testing.T) (e env, bundle []byte) {
	t.Helper()
	src, _ := bundletest.OpenDB(t)
	bundletest.Seed(t, src)
	target, path := bundletest.OpenDB(t)
	bundletest.Exec(t, target, `INSERT INTO instruments (symbol, name, market, is_active, created_at, updated_at) VALUES ('1111', 'n', 'TSE', 1, 't', 't')`)
	e = newEnv(t, router.WithMigration(migratebundle.NewExporter(src), importflow.New(target, path)))
	rec := e.session(http.MethodGet, "/api/v1/migration/export", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", rec.Code, rec.Body)
	}
	return e, rec.Body.Bytes()
}

func TestExport_ServesAScrubbedBundleToASession(t *testing.T) {
	e, bundle := wired(t)
	rec := e.session(http.MethodGet, "/api/v1/migration/export", nil, "")
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, migratebundle.Extension) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("bundle is not a two-entry ZIP: %v", err)
	}
	if bytes.Contains(bundle, []byte(bundletest.Dummy)) {
		t.Error("the downloaded bundle contains the dummy secret")
	}
}

func TestMigrationRoutes_RequireSessionAndCSRF(t *testing.T) {
	e, _ := wired(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/migration/export"},
		{http.MethodGet, "/api/v1/migration/status"},
		{http.MethodPost, "/api/v1/migration/preview"},
		{http.MethodPost, "/api/v1/migration/confirm"},
		{http.MethodPost, "/api/v1/migration/cancel"},
	} {
		if rec := e.do(tc.method, tc.path, nil, false, false, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without cookie = %d, want 403", tc.method, tc.path, rec.Code)
		}
		if tc.method == http.MethodPost {
			if rec := e.do(tc.method, tc.path, strings.NewReader("x"), true, false, ""); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s with cookie but no CSRF token = %d, want 403", tc.method, tc.path, rec.Code)
			}
		}
	}
}

// export -> preview (shows both row counts, changes nothing) -> confirm
// refused without the replace confirmation -> confirm -> status shows pending.
func TestImportFlow_PreviewConfirmStatus(t *testing.T) {
	e, bundle := wired(t)

	rec := e.session(http.MethodPost, "/api/v1/migration/preview", bytes.NewReader(bundle), "application/octet-stream")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body)
	}
	var p importflow.Preview
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Token == "" || !p.TargetHasData || p.Manifest.SecretsIncluded {
		t.Fatalf("preview = %+v", p)
	}
	var instruments importflow.TableComparison
	for _, tc := range p.Tables {
		if tc.Name == "instruments" {
			instruments = tc
		}
	}
	if instruments.Current != 1 || instruments.Bundle != 2 {
		t.Errorf("instruments = %+v, want current 1 / bundle 2", instruments)
	}

	confirm := func(replace bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"token": p.Token, "replace_existing": replace})
		return e.session(http.MethodPost, "/api/v1/migration/confirm", bytes.NewReader(body), "application/json")
	}
	if rec := confirm(false); rec.Code != http.StatusConflict {
		t.Fatalf("confirm without replace = %d, want 409: %s", rec.Code, rec.Body)
	}
	if rec := e.session(http.MethodGet, "/api/v1/migration/status", nil, ""); strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("a refused confirm left a pending import: %s", rec.Body)
	}
	if rec := confirm(true); rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", rec.Code, rec.Body)
	}
	if rec := e.session(http.MethodGet, "/api/v1/migration/status", nil, ""); !strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("status shows no pending import: %s", rec.Body)
	}
	if rec := e.session(http.MethodPost, "/api/v1/migration/cancel", nil, ""); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("cancel = %d: %s", rec.Code, rec.Body)
	}
	if rec := e.session(http.MethodGet, "/api/v1/migration/status", nil, ""); strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("status still pending after cancel: %s", rec.Body)
	}
}

func TestPreview_RefusesNonBundleWithJapaneseProblemDetail(t *testing.T) {
	e, _ := wired(t)
	rec := e.session(http.MethodPost, "/api/v1/migration/preview", strings.NewReader("not a bundle"), "application/octet-stream")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
	}
	var prob struct{ Detail string }
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil || !strings.Contains(prob.Detail, "PC 移行ファイル") {
		t.Fatalf("problem = %q (%v)", rec.Body, err)
	}
}

// An entry point that forgets router.WithMigration gets a visible 500.
func TestMigrationRoutes_FailWithoutWiring(t *testing.T) {
	e := newEnv(t)
	if rec := e.session(http.MethodGet, "/api/v1/migration/export", nil, ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("export = %d, want 500", rec.Code)
	}
	if rec := e.session(http.MethodPost, "/api/v1/migration/preview", strings.NewReader("x"), "application/octet-stream"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("preview = %d, want 500", rec.Code)
	}
}
