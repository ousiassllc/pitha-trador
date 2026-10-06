package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
)

// notFoundMessage is the fixed user-facing text of an unknown page.
const notFoundMessage = "お探しのページは見つかりませんでした。"

// noRoute answers a path or method no route handles, instead of gin's
// text/plain "404 page not found" (issue #605). It follows the same split
// as the rest of the app: `/api/v1` gets Huma's application/problem+json,
// a WebSocket upgrade a bare 404 (a page body is meaningless to the
// client), and everything else shared.RespondPageError (ErrorPage for a
// navigation, a toast for an HX-Request).
func noRoute(c *gin.Context) {
	path := c.Request.URL.Path
	switch {
	case path == apiBasePath || strings.HasPrefix(path, apiBasePath+"/"):
		body, err := json.Marshal(huma.Error404NotFound("not found"))
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Data(http.StatusNotFound, "application/problem+json", body)
	case strings.EqualFold(c.GetHeader("Upgrade"), "websocket"):
		// AbortWithStatus commits the header; a bare Status would let gin
		// append its text/plain "404 page not found".
		c.AbortWithStatus(http.StatusNotFound)
	default:
		shared.RespondPageError(c, http.StatusNotFound, notFoundMessage)
	}
}
