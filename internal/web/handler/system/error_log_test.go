package system_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/web/apierror"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/system"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// fakeErrorLogExporter records the arguments of every Export call.
type fakeErrorLogExporter struct {
	result logging.ExportResult
	err    error

	calls    int
	days     int
	minLevel slog.Level
}

func (f *fakeErrorLogExporter) Export(_ context.Context, days int, minLevel slog.Level) (logging.ExportResult, error) {
	f.calls++
	f.days, f.minLevel = days, minLevel
	return f.result, f.err
}

// errorLogRequest serves GET /api/v1/logs/errors<query> through the real
// middleware.Session and a huma API, with or without the session cookie.
func errorLogRequest(t *testing.T, exporter system.ErrorLogExporter, query string, withCookie bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	apierror.Install() // fixed-message 5xx bodies, as internal/router installs
	engine := gin.New()
	session := middleware.NewSession(nil)
	engine.Use(session.Handler())
	api := humagin.NewWithGroup(engine, engine.Group("/api/v1"), huma.DefaultConfig("test", "0"))
	huma.Get(api, "/logs/errors", system.NewErrorLogHandler(exporter).APIErrorLogs)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/errors"+query, nil)
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

func TestErrorLogHandler_ReturnsNDJSONAttachmentWithDefaults(t *testing.T) {
	body := `{"level":"ERROR","msg":"a"}` + "\n" + `{"level":"ERROR","msg":"b"}` + "\n"
	exporter := &fakeErrorLogExporter{result: logging.ExportResult{Data: []byte(body), Records: 2}}

	rec := errorLogRequest(t, exporter, "", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if exporter.days != 7 || exporter.minLevel != slog.LevelError {
		t.Errorf("defaults: days=%d level=%v, want 7 and ERROR", exporter.days, exporter.minLevel)
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("body = %q, want the exporter's NDJSON", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !regexp.MustCompile(`^attachment; filename="pitha-error-logs-\d{8}-\d{6}\.ndjson"$`).MatchString(cd) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if got := rec.Header().Get("X-Pitha-Record-Count"); got != "2" {
		t.Errorf("X-Pitha-Record-Count = %q, want 2", got)
	}
	if _, ok := rec.Header()["X-Pitha-Truncated"]; ok {
		t.Errorf("X-Pitha-Truncated = %q on an untruncated result, want the header absent", rec.Header().Get("X-Pitha-Truncated"))
	}
}

func TestErrorLogHandler_PassesQueryAndReportsTruncation(t *testing.T) {
	exporter := &fakeErrorLogExporter{result: logging.ExportResult{Data: []byte("x\n"), Records: 1, Truncated: true}}

	rec := errorLogRequest(t, exporter, "?days=90&level=warn", true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if exporter.days != 90 || exporter.minLevel != slog.LevelWarn {
		t.Errorf("export args: days=%d level=%v, want 90 and WARN", exporter.days, exporter.minLevel)
	}
	if got := rec.Header().Get("X-Pitha-Truncated"); got != "true" {
		t.Errorf("X-Pitha-Truncated = %q, want true", got)
	}
}

func TestErrorLogHandler_EmptyResultIs200WithEmptyAttachment(t *testing.T) {
	rec := errorLogRequest(t, &fakeErrorLogExporter{}, "?days=1", true)

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q, want 200 and empty body", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Pitha-Record-Count"); got != "0" {
		t.Errorf("X-Pitha-Record-Count = %q, want 0", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want an attachment even when empty", cd)
	}
}

func TestErrorLogHandler_RejectsInvalidQueryWith422(t *testing.T) {
	exporter := &fakeErrorLogExporter{}

	for _, query := range []string{"?days=0", "?days=91", "?days=-1", "?days=abc", "?level=info", "?level=ERROR"} {
		if rec := errorLogRequest(t, exporter, query, true); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("GET %s = %d, want 422 (body=%s)", query, rec.Code, rec.Body.String())
		}
	}
	for _, query := range []string{"?days=1", "?days=90", "?level=error", "?level=warn"} {
		if rec := errorLogRequest(t, exporter, query, true); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (body=%s)", query, rec.Code, rec.Body.String())
		}
	}
	if exporter.calls != 4 {
		t.Errorf("exporter called %d times, want only for the 4 valid requests", exporter.calls)
	}
}

func TestErrorLogHandler_FailureIsFixedMessage500(t *testing.T) {
	cause := errors.New(`open /secret/path/logs: permission denied`)

	rec := errorLogRequest(t, &fakeErrorLogExporter{err: cause}, "", true)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/secret/path") || strings.Contains(rec.Body.String(), "permission denied") {
		t.Errorf("500 body leaks the cause: %s", rec.Body.String())
	}
}

func TestErrorLogHandler_RequiresSessionCookie(t *testing.T) {
	exporter := &fakeErrorLogExporter{}

	rec := errorLogRequest(t, exporter, "", false)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without the session cookie", rec.Code)
	}
	if exporter.calls != 0 {
		t.Errorf("exporter called %d times for an unauthenticated request, want 0", exporter.calls)
	}
}
