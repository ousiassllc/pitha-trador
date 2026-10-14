// Package exportroute holds the router-level tests of GET /api/v1/export
// (issue #773); the handler itself is covered in internal/web/handler/export.
package exportroute_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
	gin.SetMode(gin.TestMode)
	engine := router.New(opts...)
	req := httptest.NewRequest(http.MethodGet, exportURL, nil)
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
