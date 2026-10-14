package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
	"github.com/ousiassllc/pitha-trador/internal/web/apierror"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/export"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// fakeExporter records the request and writes a one-entry ZIP.
type fakeExporter struct {
	err   error
	calls int
	req   dataexport.Request
}

func (f *fakeExporter) Export(_ context.Context, w io.Writer, req dataexport.Request) (dataexport.Manifest, error) {
	f.calls++
	f.req = req
	if f.err != nil {
		return dataexport.Manifest{}, f.err
	}
	zw := zip.NewWriter(w)
	fw, _ := zw.Create("manifest.json")
	_, _ = fw.Write([]byte("{}"))
	return dataexport.Manifest{}, zw.Close()
}

// exportRequest serves GET /api/v1/export<query> through the real
// middleware.Session and a huma API, with or without the session cookie.
func exportRequest(t *testing.T, exporter export.DataExporter, query string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	apierror.Install()
	engine := gin.New()
	session := middleware.NewSession(nil)
	engine.Use(session.Handler())
	api := humagin.NewWithGroup(engine, engine.Group("/api/v1"), huma.DefaultConfig("test", "0"))
	huma.Get(api, "/export", export.NewHandler(exporter).API)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/export"+query, nil)
	if withCookie {
		boot := httptest.NewRecorder()
		engine.ServeHTTP(boot, httptest.NewRequest(http.MethodGet, "/", nil))
		for _, c := range boot.Result().Cookies() {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

const validQuery = "?from=2026-09-28&to=2026-09-30&kinds=trades&kinds=metrics"

func TestHandler_ReturnsZipAttachmentForInclusiveUTCDates(t *testing.T) {
	exporter := &fakeExporter{}
	rec := exportRequest(t, exporter, validQuery, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q", got)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, `attachment; filename="pitha-export-`) || !strings.HasSuffix(cd, `.zip"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != "manifest.json" {
		t.Fatalf("body is not the exported archive: %v", err)
	}
	if got := rec.Header().Get("Content-Length"); got != "" && got != strconv.Itoa(rec.Body.Len()) {
		t.Errorf("Content-Length = %s, body = %d bytes", got, rec.Body.Len())
	}
	wantFrom := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) // to is included: [from, to+1 day)
	if !exporter.req.Period.From.Equal(wantFrom) || !exporter.req.Period.To.Equal(wantTo) {
		t.Errorf("period = %v, want [%v, %v)", exporter.req.Period, wantFrom, wantTo)
	}
	if len(exporter.req.Kinds) != 2 || exporter.req.Kinds[0] != dataexport.KindTrades || exporter.req.Kinds[1] != dataexport.KindMetrics {
		t.Errorf("kinds = %v", exporter.req.Kinds)
	}
	if exporter.req.Format != dataexport.FormatCSV {
		t.Errorf("format = %q, want the csv default", exporter.req.Format)
	}
}

func TestHandler_PassesJSONLFormat(t *testing.T) {
	exporter := &fakeExporter{}
	if rec := exportRequest(t, exporter, validQuery+"&format=jsonl", true); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if exporter.req.Format != dataexport.FormatJSONL {
		t.Errorf("format = %q, want jsonl", exporter.req.Format)
	}
}

func TestHandler_RejectsBadQueryWith422(t *testing.T) {
	for name, query := range map[string]string{
		"missing from":       "?to=2026-09-30&kinds=trades",
		"missing to":         "?from=2026-09-28&kinds=trades",
		"missing kinds":      "?from=2026-09-28&to=2026-09-30",
		"malformed date":     "?from=2026-9-28&to=2026-09-30&kinds=trades",
		"unknown kind":       "?from=2026-09-28&to=2026-09-30&kinds=secrets",
		"unknown format":     "?from=2026-09-28&to=2026-09-30&kinds=trades&format=xml",
		"from after to":      "?from=2026-09-30&to=2026-09-28&kinds=trades",
		"period too long":    "?from=2025-01-01&to=2026-09-30&kinds=trades",
		"period 367 days":    "?from=2026-01-01&to=2027-01-02&kinds=trades",
		"unsupported 0 kind": "?from=2026-09-28&to=2026-09-30&kinds=",
	} {
		exporter := &fakeExporter{}
		rec := exportRequest(t, exporter, query, true)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422 (body %s)", name, rec.Code, rec.Body)
		}
		if exporter.calls != 0 {
			t.Errorf("%s: exporter called %d times, want 0", name, exporter.calls)
		}
	}
}

func TestHandler_AcceptsLongestPeriod(t *testing.T) {
	exporter := &fakeExporter{}
	rec := exportRequest(t, exporter, "?from=2026-01-01&to=2027-01-01&kinds=trades", true) // 366 days
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestHandler_WithoutSessionCookieIs403(t *testing.T) {
	exporter := &fakeExporter{}
	rec := exportRequest(t, exporter, validQuery, false)
	if rec.Code != http.StatusForbidden || exporter.calls != 0 {
		t.Fatalf("status = %d, calls = %d, want 403 and no export", rec.Code, exporter.calls)
	}
}

func TestHandler_ExportFailureIsFixed500WithoutCause(t *testing.T) {
	rec := exportRequest(t, &fakeExporter{err: errors.New("open /secret/path: boom")}, validQuery, true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("500 body leaks the cause: %s", rec.Body)
	}
	if strings.Contains(rec.Header().Get("Content-Type"), "zip") {
		t.Error("a failed export must not answer with a zip")
	}
}

func TestHandler_ServiceInvalidRequestIs422(t *testing.T) {
	rec := exportRequest(t, &fakeExporter{err: dataexport.ErrInvalidRequest}, validQuery, true)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestUnconfiguredExporterIs500(t *testing.T) {
	rec := exportRequest(t, export.UnconfiguredExporter{}, validQuery, true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
