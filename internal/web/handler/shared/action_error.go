package shared

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// RespondActionError answers a failed HTMX action with status and an
// atoms.Toast fragment carrying message (docs/components/overview.md §4
// "エラー表示"). HTMX does not swap 4xx/5xx by default; layout.Shell's
// `htmx-config` routes exactly these fragments into `#toast-region`, so the
// user sees why the action failed instead of a button that silently did
// nothing.
func RespondActionError(c *gin.Context, status int, message string) {
	renderErrorBody(c, status, atoms.Toast(message), message)
}

// RespondFormError answers a rejected form submission (a validation error: a
// 422 and nothing saved) with an atoms.InlineAlert for the form's own error
// slot instead of a toast (design guide O3: validation never goes to a toast).
// errorTarget is the CSS selector of that slot, an element inside the form
// that stays on the page (e.g. `#jev-prompt-shadow-error`); the HX-Retarget /
// HX-Reswap headers swap the alert there rather than into the form's
// hx-target, which would replace the whole form and lose the input.
// layout.Shell's `htmx-config` lets a 422 swap (`error: true`), and
// pitha-htmx-errors passes a body carrying `data-inline-alert` through.
// message is user-facing text with no error text or secret in it: a fixed
// string, or a fixed string followed by a validation reason the domain
// package wrote for the operator (e.g. experiment.IsInvalid), never err.Error().
func RespondFormError(c *gin.Context, errorTarget, message string) {
	c.Header("HX-Retarget", errorTarget)
	c.Header("HX-Reswap", "innerHTML")
	renderErrorBody(c, http.StatusUnprocessableEntity, atoms.InlineAlert("入力を確認してください", message), message)
}

// RespondPageError answers a failed SSR page route (issue #143): an
// HX-Request gets the RespondActionError toast, a full-page navigation gets
// pages.ErrorPage, so neither ends in an empty body. message MUST be a
// fixed user-facing string; log the underlying error with slog at the call
// site instead of exposing err.Error().
func RespondPageError(c *gin.Context, status int, message string) {
	if c.GetHeader("HX-Request") == "true" {
		RespondActionError(c, status, message)
		return
	}
	RenderErrorPage(c, status, message)
}

// RenderErrorPage writes pages.ErrorPage with status. It is also the
// middleware.ErrorPageRenderer internal/router injects into Recovery and
// Session (issue #171), which sit outside the handler chain and cannot
// import pages themselves.
func RenderErrorPage(c *gin.Context, status int, message string) {
	renderErrorBody(c, status, pages.ErrorPage(status, message), message)
}
