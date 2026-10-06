package router_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/router"
)

func serveNoRoute(t *testing.T, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.New().ServeHTTP(rec, req)
	return rec
}

func TestNoRoute_UnknownPageRendersErrorPage(t *testing.T) {
	for _, target := range []string{"/nope", "/positions/1/close"} { // 2nd: wrong method (GET on a POST route)
		rec := serveNoRoute(t, http.MethodGet, target, map[string]string{"Accept": "text/html"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s: Content-Type = %q, want text/html", target, ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "404 Not Found") || !strings.Contains(body, "<html") {
			t.Errorf("%s: body is not pages.ErrorPage(404): %q", target, body)
		}
	}
}

func TestNoRoute_UnknownAPIPathReturnsProblemJSON(t *testing.T) {
	for _, target := range []string{"/api/v1/nope", "/api/v1"} {
		rec := serveNoRoute(t, http.MethodGet, target, map[string]string{"Accept": "text/html"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q, want application/problem+json", target, ct)
		}
		var body struct {
			Status int    `json:"status"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body is not JSON: %v (%q)", target, err, rec.Body.String())
		}
		if body.Status != http.StatusNotFound || body.Title != "Not Found" {
			t.Errorf("%s: body = %+v, want status 404 / Not Found", target, body)
		}
	}
}

func TestNoRoute_HTMXRequestGetsToastFragment(t *testing.T) {
	rec := serveNoRoute(t, http.MethodGet, "/nope", map[string]string{"HX-Request": "true"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") || !strings.Contains(rec.Body.String(), "見つかりません") {
		t.Errorf("body = %q, want a toast fragment", rec.Body.String())
	}
}

func TestNoRoute_UnknownWebSocketPathIsBare404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := router.New()
	page := httptest.NewRecorder()
	engine.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/scanner", nil))
	if len(page.Result().Cookies()) != 1 {
		t.Fatalf("GET /scanner set %d cookies, want the session cookie", len(page.Result().Cookies()))
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/nope", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.AddCookie(page.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}
