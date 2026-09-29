package insightapi

import "github.com/danielgtaylor/huma/v2"

const defaultListLimit = 100

// Handler implements the routes documented in the package comment.
type Handler struct {
	provider Provider
}

// New returns a Handler backed by provider.
func New(provider Provider) *Handler {
	return &Handler{provider: provider}
}

// Register mounts the four routes on api (internal/router's `/api/v1`
// Huma API), so they appear in the generated OpenAPI document.
func (h *Handler) Register(api huma.API) {
	huma.Get(api, "/symbols/{symbol}/decisions", h.Decisions)
	huma.Get(api, "/signals", h.Signals)
	huma.Get(api, "/signals/{symbol}", h.SymbolSignals)
	huma.Get(api, "/performance", h.Performance)
}
