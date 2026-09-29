package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// captureLogs swaps slog.Default for a JSON logger writing to the returned
// buffer for the duration of the test, and returns the decoded records.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []map[string]any {
		var records []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("log line %q is not JSON: %v", line, err)
			}
			records = append(records, rec)
		}
		return records
	}
}

func loggedEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.RequestLog(), middleware.Recovery(nil))
	engine.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	engine.GET("/missing", func(c *gin.Context) { c.Status(http.StatusNotFound) })
	engine.GET("/boom", func(*gin.Context) { panic("kaboom") })
	engine.GET("/abort", func(*gin.Context) { panic(http.ErrAbortHandler) })
	engine.GET("/static/x", func(c *gin.Context) { c.String(http.StatusOK, "x") })
	return engine
}

func TestRecovery_PanicBecomes500AndIsLoggedWithStackAndFinalStatus(t *testing.T) {
	logs := captureLogs(t)
	engine := loggedEngine()

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom?token=secret", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	records := logs()
	if len(records) != 2 {
		t.Fatalf("got %d log records, want panic + access log: %v", len(records), records)
	}
	panicRec, accessRec := records[0], records[1]
	if panicRec["level"] != "ERROR" || panicRec["panic"] != "kaboom" || panicRec["path"] != "/boom" {
		t.Errorf("panic record = %v", panicRec)
	}
	if stack, _ := panicRec["stack"].(string); !strings.Contains(stack, "recovery.go") {
		t.Errorf("panic record has no stack trace: %v", panicRec["stack"])
	}
	if accessRec["status"] != float64(500) || accessRec["level"] != "ERROR" {
		t.Errorf("access record = %v, want status 500 at ERROR", accessRec)
	}
}

func TestRecovery_ServerKeepsServingAfterPanic(t *testing.T) {
	captureLogs(t)
	engine := loggedEngine()

	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("after panic: status = %d body = %q, want 200 ok", rec.Code, rec.Body.String())
	}
}

func TestRecovery_RepanicsErrAbortHandler(t *testing.T) {
	captureLogs(t)
	engine := loggedEngine()

	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", r)
		}
	}()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
}

func TestRequestLog_LogsMethodPathRouteStatusAndLevelWithoutQuery(t *testing.T) {
	logs := captureLogs(t)
	engine := loggedEngine()

	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok?token=secret", nil))
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	records := logs()
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2: %v", len(records), records)
	}
	ok, missing := records[0], records[1]
	if ok["method"] != "GET" || ok["path"] != "/ok" || ok["route"] != "/ok" || ok["status"] != float64(200) || ok["level"] != "INFO" {
		t.Errorf("ok record = %v", ok)
	}
	if _, has := ok["latency_ms"]; !has {
		t.Errorf("ok record has no latency_ms: %v", ok)
	}
	if strings.Contains(ok["path"].(string), "secret") {
		t.Errorf("query string leaked into log: %v", ok)
	}
	if missing["status"] != float64(404) || missing["level"] != "WARN" {
		t.Errorf("missing record = %v, want status 404 at WARN", missing)
	}
}

func TestRequestLog_SkipsStaticAssets(t *testing.T) {
	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	loggedEngine().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if records := logs(); len(records) != 0 {
		t.Fatalf("static request logged: %v", records)
	}
}
