package apierrorpage_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware/apierrorpage"
)

const testErrorPageMarker = "test-error-page"

// renderTestPage stands in for shared.RenderErrorPage (pages imports
// middleware, so a middleware test cannot use the real one).
func renderTestPage(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_, _ = c.Writer.WriteString("<p " + testErrorPageMarker + ">" + message + "</p>")
}

func do(engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func withHeaders(req *http.Request, headers map[string]string) *http.Request {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

var (
	navigation = map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"}
	htmx       = map[string]string{"Accept": "text/html", "HX-Request": "true"}
	fetchJSON  = map[string]string{"Accept": "*/*"}
)

const problemBody = `{"title":"Unprocessable Entity","detail":"kinds is required"}`

func apiErrorEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.Recovery(renderTestPage), apierrorpage.Handler(renderTestPage))
	engine.GET("/api/v1/fail", func(c *gin.Context) {
		c.Header("Content-Type", "application/problem+json")
		c.Header("Retry-After", "5")
		c.Status(http.StatusUnprocessableEntity)
		_, _ = c.Writer.WriteString(problemBody)
		c.Writer.Flush()
	})
	engine.GET("/api/v1/zip", func(c *gin.Context) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Length", "10")
		c.Status(http.StatusOK)
		_, _ = c.Writer.WriteString("PK")
		c.Writer.Flush()
		_, _ = c.Writer.Write([]byte("12345678"))
	})
	engine.GET("/api/v1/boom", func(*gin.Context) { panic("kaboom") })
	engine.GET("/fail", func(c *gin.Context) { c.String(http.StatusInternalServerError, "plain") })
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusNotFound, problemBody) })
	return engine
}

func TestHandler_NavigationFailureGetsErrorPage(t *testing.T) {
	rec := do(apiErrorEngine(), withHeaders(httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil), navigation))

	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), testErrorPageMarker) {
		t.Fatalf("status=%d body=%q, want 422 with the error page", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "kinds is required") {
		t.Errorf("body leaks the problem detail: %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); strings.Contains(got, "problem+json") {
		t.Errorf("Content-Type = %q, want the page's", got)
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want the handler's header kept", got)
	}
}

func TestHandler_OtherClientsKeepProblemJSON(t *testing.T) {
	engine := apiErrorEngine()
	cases := map[string]map[string]string{
		"fetch":     fetchJSON,
		"json":      {"Accept": "application/json"},
		"no Accept": {},
		"htmx":      htmx,
		"websocket": {"Accept": "text/html", "Upgrade": "websocket"},
	}
	for name, headers := range cases {
		rec := do(engine, withHeaders(httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil), headers))
		if rec.Code != http.StatusUnprocessableEntity || rec.Body.String() != problemBody {
			t.Errorf("%s: status=%d body=%q, want 422 with the untouched problem body", name, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q, want application/problem+json", name, got)
		}
	}
}

func TestHandler_UnknownAPIPathStaysProblemBody(t *testing.T) {
	rec := do(apiErrorEngine(), withHeaders(httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil), navigation))

	if rec.Code != http.StatusNotFound || rec.Body.String() != problemBody {
		t.Fatalf("status=%d body=%q, want the router's own 404", rec.Code, rec.Body.String())
	}
}

func TestHandler_NonAPIPathIsUntouched(t *testing.T) {
	rec := do(apiErrorEngine(), withHeaders(httptest.NewRequest(http.MethodGet, "/fail", nil), navigation))

	if rec.Code != http.StatusInternalServerError || rec.Body.String() != "plain" {
		t.Fatalf("status=%d body=%q, want the handler's own 500", rec.Code, rec.Body.String())
	}
}

func TestHandler_SuccessfulStreamPassesThrough(t *testing.T) {
	rec := do(apiErrorEngine(), withHeaders(httptest.NewRequest(http.MethodGet, "/api/v1/zip", nil), navigation))

	if rec.Code != http.StatusOK || rec.Body.String() != "PK12345678" {
		t.Fatalf("status=%d body=%q, want 200 PK12345678", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/zip" || rec.Header().Get("Content-Length") != "10" {
		t.Errorf("headers = %v, want Content-Type application/zip and Content-Length 10", rec.Header())
	}
	if !rec.Flushed {
		t.Error("Flush did not reach the underlying writer")
	}
}

// A panic must reach Recovery with the real writer, not the wrapper that
// withholds a >= 400 status.
func TestHandler_PanicStillAnswers500(t *testing.T) {
	rec := do(apiErrorEngine(), withHeaders(httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil), navigation))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandler_NilRendererDisablesIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(apierrorpage.Handler(nil))
	engine.GET("/api/v1/fail", func(c *gin.Context) { c.String(http.StatusBadRequest, problemBody) })

	rec := do(engine, withHeaders(httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil), navigation))
	if rec.Code != http.StatusBadRequest || rec.Body.String() != problemBody {
		t.Fatalf("status=%d body=%q, want the untouched 400", rec.Code, rec.Body.String())
	}
}

// Over a real connection the wrapper must still let a handler hijack (the
// WebSocket upgrade path) through http.ResponseController.
func TestHandler_ForwardsHijack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(apierrorpage.Handler(renderTestPage))
	engine.GET("/api/v1/stream", func(c *gin.Context) {
		conn, rw, err := http.NewResponseController(c.Writer).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		_ = rw.Flush()
	})
	srv := httptest.NewServer(engine)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("status=%d body=%q err=%v, want the hijacked 200 ok", resp.StatusCode, body, err)
	}
}
