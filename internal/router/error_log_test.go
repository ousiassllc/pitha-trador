package router_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// #736: the real exporter behind GET /api/v1/logs/errors never emits the
// 立花 認証ID・秘密鍵・第二暗証番号・仮想URL.
func TestNew_ErrorLogDownloadMasksTachibanaSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	now := time.Now().UTC()
	record := fmt.Sprintf(`{"time":%q,"level":"ERROR","msg":"tachibana login failed","sAuthId":"AUTH-SECRET","private_key":"PEM-SECRET","second":"SECOND-SECRET","error":"Post \"https://kabuka.e-shiten.jp/e_api_v4r10/request/MjAyNjEwMDgwMDAwMDBUT0tFTjEyMzQ1Ng/\": EOF"}`+"\n", now.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, now.Format("2006-01-02")+".log"), []byte(record), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	engine := router.New(router.WithErrorLogExporter(logging.NewExporter(dir)))
	req := authorize(t, engine, httptest.NewRequest(http.MethodGet, "/api/v1/logs/errors", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tachibana login failed") {
		t.Fatalf("status=%d, want 200 with the record; body: %s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"AUTH-SECRET", "PEM-SECRET", "SECOND-SECRET", "MjAyNjEwMDgw"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("secret %q in the download: %s", secret, rec.Body.String())
		}
	}
}
