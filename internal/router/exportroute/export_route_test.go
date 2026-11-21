// Package exportroute holds the router-level tests of GET /api/v1/export
// (issue #773); the handler itself is covered in internal/web/handler/export.
package exportroute_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

// emptyDataExporter stands in for the *dataexport.Service bootstrap injects.
type emptyDataExporter struct{}

func (emptyDataExporter) Export(_ context.Context, w io.Writer, _ dataexport.Request) (dataexport.Manifest, error) {
	_, err := w.Write([]byte("PK"))
	return dataexport.Manifest{}, err
}

const exportURL = "/api/v1/export?from=2026-09-28&to=2026-09-30&kinds=trades"

// get serves exportURL on a router built with opts, with the session cookie a
// browser gets from loading any page first when withCookie is set.
func get(t *testing.T, withCookie bool, opts ...router.Option) *httptest.ResponseRecorder {
	t.Helper()
	return getURL(t, exportURL, "", withCookie, opts...)
}

// getURL is get for target with the given Accept header ("" sends none).
func getURL(t *testing.T, target, accept string, withCookie bool, opts ...router.Option) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := router.New(opts...)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if withCookie {
		page := httptest.NewRecorder()
		engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup", nil))
		for _, c := range page.Result().Cookies() {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestNew_ServesDataExportToASessionWithTheInjectedExporter(t *testing.T) {
	rec := get(t, true, router.WithDataExporter(emptyDataExporter{}))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status = %d, type = %q, want 200 application/zip; body: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

// An entry point that forgets router.WithDataExporter gets a visible 500, not
// an empty archive.
func TestNew_DataExportFailsWithoutInjectedExporter(t *testing.T) {
	if rec := get(t, true); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body)
	}
}

// The export holds trades and decisions: without the session cookie it is 403
// even though the method is a safe GET.
func TestNew_DataExportRequiresSessionCookie(t *testing.T) {
	if rec := get(t, false, router.WithDataExporter(emptyDataExporter{})); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// Issue #1196: the Settings download form is a plain browser navigation, so a
// rejected export is the HTML error page (same status, fixed text); the same
// request from an API client keeps the problem+json.
func TestNew_DataExportFailureIsErrorPageForNavigationOnly(t *testing.T) {
	const noKinds = "/api/v1/export?from=2026-09-28&to=2026-09-30"
	exporter := router.WithDataExporter(emptyDataExporter{})

	page := getURL(t, noKinds, "text/html,application/xhtml+xml,*/*;q=0.8", true, exporter)
	if page.Code != http.StatusUnprocessableEntity {
		t.Fatalf("navigation status = %d, want 422; body: %s", page.Code, page.Body)
	}
	if ct := page.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("navigation Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(page.Body.String(), `data-testid="error-page"`) {
		t.Errorf("navigation body = %s, want the pages.ErrorPage", page.Body)
	}

	api := getURL(t, noKinds, "application/json", true, exporter)
	if api.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(api.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("API status = %d, type = %q, want 422 problem+json; body: %s", api.Code, api.Header().Get("Content-Type"), api.Body)
	}
}

// A successful ZIP download is untouched by the navigation error page.
func TestNew_DataExportZipIsIntactForNavigation(t *testing.T) {
	rec := getURL(t, exportURL, "text/html", true, router.WithDataExporter(emptyDataExporter{}))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" || rec.Body.String() != "PK" {
		t.Fatalf("status = %d, type = %q, body = %q, want 200 application/zip PK", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
}

// Without the session cookie the 403 is the error page for a navigation.
func TestNew_DataExportForbiddenIsErrorPageForNavigation(t *testing.T) {
	rec := getURL(t, exportURL, "text/html", false, router.WithDataExporter(emptyDataExporter{}))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `data-testid="error-page"`) {
		t.Fatalf("status = %d, want 403 error page; body: %s", rec.Code, rec.Body)
	}
}
