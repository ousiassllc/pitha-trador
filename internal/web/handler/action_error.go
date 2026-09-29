package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
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
