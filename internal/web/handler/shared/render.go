package shared

import (
	"bytes"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

const htmlContentType = "text/html; charset=utf-8"

// renderBuffered renders comp fully before anything reaches the client, so a
// failing component can still be answered with an error status instead of a
// truncated 200. A failure is logged here (FR-ERRLOG) and reported as false.
func renderBuffered(c *gin.Context, comp templ.Component) ([]byte, bool) {
	var buf bytes.Buffer
	if err := comp.Render(c.Request.Context(), &buf); err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: render", "path", c.FullPath(), "error", err)
		return nil, false
	}
	return buf.Bytes(), true
}

// RenderHTML answers with comp as an HTML body and status. If rendering fails
// the partial output is discarded, the error is logged and the request gets
// RespondPageError's 500 instead (a toast for an HX-Request, ErrorPage for a
// full-page navigation), so a broken template never ships as a cut-off 200.
func RenderHTML(c *gin.Context, status int, comp templ.Component) {
	body, ok := renderBuffered(c, comp)
	if !ok {
		RespondPageError(c, http.StatusInternalServerError, "画面の描画に失敗しました。")
		return
	}
	c.Data(status, htmlContentType, body)
}

// renderErrorBody is RenderHTML for the error responders themselves, which
// must not recurse into RespondPageError when their own template fails: it
// falls back to message as plain text with the original status.
func renderErrorBody(c *gin.Context, status int, comp templ.Component, message string) {
	body, ok := renderBuffered(c, comp)
	if !ok {
		c.String(status, message)
		return
	}
	c.Data(status, htmlContentType, body)
}
