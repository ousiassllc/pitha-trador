package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

// Downloading the error log is operator activity (FR-ERRLOG-6); the handler
// itself is covered in internal/web/handler/system.
func TestNew_ErrorLogDownloadCountsAsOperatorActivity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &countingRecorder{}
	engine := router.New(router.WithHeartbeatRecorder(recorder))
	req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/api/v1/logs/errors", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || recorder.calls != 1 {
		t.Fatalf("status=%d heartbeats=%d, want 200 and 1", rec.Code, recorder.calls)
	}
}
