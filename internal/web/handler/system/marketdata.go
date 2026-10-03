package system

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// MarketDataStatusSource is what `GET /system/marketdata-status` needs from
// the kabuステーションAPI client: the outcome of its last token issuance.
// *marketdata.Client implements it directly.
type MarketDataStatusSource interface {
	TokenStatus() marketdata.TokenStatus
}

// MarketDataHandler implements `GET /system/marketdata-status` (issue
// #295). A nil source (router-level tests) renders nothing.
type MarketDataHandler struct {
	source MarketDataStatusSource
}

// NewMarketDataHandler returns a MarketDataHandler backed by source.
func NewMarketDataHandler(source MarketDataStatusSource) *MarketDataHandler {
	return &MarketDataHandler{source: source}
}

// Status implements `GET /system/marketdata-status`: Header's
// `#marketdata-banner` fragment (organisms.MarketDataBanner), non-empty
// only while the last kabuステーションAPI token issuance failed.
func (h *MarketDataHandler) Status(c *gin.Context) {
	var issue, guidance string
	if h.source != nil {
		status := h.source.TokenStatus()
		issue, guidance = string(status.Issue), status.Guidance()
	}
	shared.RenderHTML(c, http.StatusOK, organisms.MarketDataBanner(issue, guidance))
}
