package router_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/router"
)

// emptyErrorLogExporter reports an empty log, standing in for the real
// *logging.Exporter cmd/desktop and cmd/server inject.
type emptyErrorLogExporter struct{}

func (emptyErrorLogExporter) Export(context.Context, int, slog.Level) (logging.ExportResult, error) {
	return logging.ExportResult{}, nil
}

// Downloading the error log is operator activity (FR-ERRLOG-6); the handler
// itself is covered in internal/web/handler/system.
func TestNew_ErrorLogDownloadCountsAsOperatorActivity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &countingRecorder{}
	engine := router.New(router.WithHeartbeatRecorder(recorder), router.WithErrorLogExporter(emptyErrorLogExporter{}))
	req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/api/v1/logs/errors", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || recorder.calls != 1 {
		t.Fatalf("status=%d heartbeats=%d, want 200 and 1", rec.Code, recorder.calls)
	}
}

// issue #290: an entry point that forgets router.WithErrorLogExporter must
// get a visible 500, not an empty 200 that reads as "no errors logged".
func TestNew_ErrorLogDownloadFailsWithoutInjectedExporter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/api/v1/logs/errors", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
}
