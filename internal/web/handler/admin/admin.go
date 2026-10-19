// Package admin serves the admin hub (`GET /admin`, issue #793): the
// categorised index of every admin screen and Settings dialog
// (docs/components/overview.md §画面一覧).
package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/adminhub"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// Handler implements `GET /admin`.
type Handler struct {
	swagger bool
}

// NewHandler returns a Handler. swagger is whether `/swagger` is registered
// (SWAGGER_ENABLED); only then does the hub list the API documentation.
func NewHandler(swagger bool) *Handler { return &Handler{swagger: swagger} }

// Page renders the admin hub.
func (h *Handler) Page(c *gin.Context) {
	shared.RenderHTML(c, http.StatusOK, pages.AdminPage(adminhub.Visible(h.swagger)))
}
