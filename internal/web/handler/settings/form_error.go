package settings

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

// respondInvalidInput answers a rejected Settings row form (a blank or invalid
// value: nothing is saved). An HTMX request gets an inline alert in the row's
// own error slot (design guide O3: validation never goes to a toast);
// elementID is that slot's id (molecules.SecretErrorID / SettingErrorID).
// A plain form post (JS disabled) gets the full error page with a 400, since
// it has no slot to swap into. message MUST be a fixed user-facing string.
func respondInvalidInput(c *gin.Context, elementID, message string) {
	if c.GetHeader("HX-Request") != "true" {
		shared.RespondPageError(c, http.StatusBadRequest, message)
		return
	}
	// Attribute selector: setting keys contain dots, which `#id` would read as classes.
	shared.RespondFormError(c, `[id="`+elementID+`"]`, message)
}
