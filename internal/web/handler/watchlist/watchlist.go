package watchlist

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// Source supplies the Watchlist screen (internal/bootstrap/tachibanawatch's
// Viewer implements it).
type Source interface {
	WatchLists(ctx context.Context) (domain.WatchListView, error)
}

// StaticSource is the idle Source (no list is maintained), the default of
// internal/router.New() until a real one is wired in.
type StaticSource struct{}

// WatchLists implements Source.
func (StaticSource) WatchLists(context.Context) (domain.WatchListView, error) {
	return domain.WatchListView{}, nil
}

// Handler implements `GET /watchlist`.
type Handler struct {
	source Source
}

// NewHandler returns a Handler backed by source.
func NewHandler(source Source) *Handler { return &Handler{source: source} }

// Page renders the Watchlist screen.
func (h *Handler) Page(c *gin.Context) {
	view, err := h.source.WatchLists(c.Request.Context())
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: watchlist page", "error", err)
		shared.RespondPageError(c, http.StatusInternalServerError, "監視リストの取得に失敗しました。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, pages.WatchlistPage(view))
}
