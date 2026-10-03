package shared_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

// failingAfterWrite writes a partial fragment and then fails, like a
// template whose child component errors mid-render.
func failingAfterWrite() templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "<div>partial")
		return errors.New("child component failed")
	})
}

func serveRender(t *testing.T, comp templ.Component, hxRequest bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/x", func(c *gin.Context) { shared.RenderHTML(c, http.StatusOK, comp) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if hxRequest {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// Issue #308: a Render error used to be dropped after the 200 header was
// already out, so the client got a truncated fragment and nothing was
// logged. RenderHTML must discard the partial output, log, and answer 500.
func TestRenderHTML_RenderFailureIs500AndLogged(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for name, hx := range map[string]bool{"full page": false, "htmx fragment": true} {
		t.Run(name, func(t *testing.T) {
			logs.Reset()
			rec := serveRender(t, failingAfterWrite(), hx)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "partial") {
				t.Errorf("partial output leaked into response: %q", rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "画面の描画に失敗しました。") {
				t.Errorf("body = %q, want the render failure message", rec.Body.String())
			}
			if !strings.Contains(logs.String(), "child component failed") {
				t.Errorf("render error not logged: %q", logs.String())
			}
		})
	}
}

func TestRenderHTML_SuccessWritesStatusAndBody(t *testing.T) {
	comp := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<p>ok</p>")
		return err
	})
	rec := serveRender(t, comp, true)
	if rec.Code != http.StatusOK || rec.Body.String() != "<p>ok</p>" {
		t.Fatalf("got %d %q, want 200 <p>ok</p>", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}
