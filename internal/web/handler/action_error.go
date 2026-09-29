package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// respondActionError answers a failed HTMX action with status and an
// atoms.Toast fragment carrying message (docs/components/overview.md §4
// "エラー表示"). HTMX does not swap 4xx/5xx by default; layout.Shell's
// `htmx-config` routes exactly these fragments into `#toast-region`, so the
// user sees why the action failed instead of a button that silently did
// nothing.
func respondActionError(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_ = atoms.Toast(message).Render(c.Request.Context(), c.Writer)
}

// respondPageError answers a failed SSR page route (issue #143): an
// HX-Request gets the respondActionError toast, a full-page navigation gets
// pages.ErrorPage, so neither ends in an empty body. message MUST be a
// fixed user-facing string; log the underlying error with slog at the call
// site instead of exposing err.Error().
func respondPageError(c *gin.Context, status int, message string) {
	if c.GetHeader("HX-Request") == "true" {
		respondActionError(c, status, message)
		return
	}
	RenderErrorPage(c, status, message)
}

// RenderErrorPage writes pages.ErrorPage with status. It is also the
// middleware.ErrorPageRenderer internal/router injects into Recovery and
// Session (issue #171), which sit outside the handler chain and cannot
// import pages themselves.
func RenderErrorPage(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_ = pages.ErrorPage(status, message).Render(c.Request.Context(), c.Writer)
}
