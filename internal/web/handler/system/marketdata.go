package system

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// versionRetiringNotice is the banner heads-up while the broker has announced
// an API release (立花 e支店, issue #727): not a failure, so the connection
// banner stays out of the way of the real errors.
const versionRetiringNotice = "証券会社のAPIの更新（リリース予定日）が告知されています。変更内容を確認し、接続先URLの版数を更新してください。"

// MarketDataStatusSource is what `GET /system/marketdata-status` needs from
// the broker adapter: the state of its session (broker.Session).
type MarketDataStatusSource interface {
	Status() broker.SessionStatus
}

// MarketDataHandler implements `GET /system/marketdata-status` (issues #295,
// #739). A nil source (router-level tests) renders nothing.
type MarketDataHandler struct {
	source      MarketDataStatusSource
	selection   BrokerSelection
	watchNotice WatchNoticeSource
}

// WatchNoticeSource tells whether the 立花 watch list in use is a stand-in
// because the nightly daily bars could not be used (issue #730): the operator
// notice to show on the banner, or "" when the list is the planned one.
type WatchNoticeSource interface {
	WatchNotice(ctx context.Context) string
}

// WithWatchNotice adds the watch list fallback notice to the banner. It
// returns h for chaining.
func (h *MarketDataHandler) WithWatchNotice(source WatchNoticeSource) *MarketDataHandler {
	h.watchNotice = source
	return h
}

// NewMarketDataHandler returns a MarketDataHandler backed by source.
func NewMarketDataHandler(source MarketDataStatusSource) *MarketDataHandler {
	return &MarketDataHandler{source: source}
}

// WithBrokerSelection lets the banner follow the broker selection: with 立花
// selected it words the failure per cause and names the demo / production
// environment (issue #739). Without it (or with kabu selected) the banner is
// the kabu one. It returns h for chaining.
func (h *MarketDataHandler) WithBrokerSelection(selection BrokerSelection) *MarketDataHandler {
	h.selection = selection
	return h
}

// Status implements `GET /system/marketdata-status`: Header's
// `#marketdata-banner` fragment (organisms.MarketDataBanner), non-empty
// only while the broker session is failing (kabu: the last token issuance
// failed), and escalated while a not_logged_in streak persists (issue #712).
// With 立花 selected the guidance is tachibanaGuidance's, the banner always
// names the environment and the kabu-specific escalation does not apply.
func (h *MarketDataHandler) Status(c *gin.Context) {
	var props organisms.MarketDataBannerProps
	if h.source != nil {
		status := h.source.Status()
		props.Issue, props.Guidance = string(status.Issue), status.Guidance
		props.Failures = status.Failures
		props.Persistent, props.Elapsed = status.Persistent(time.Now())
		if status.Failed() {
			if environment := h.tachibanaEnvironment(c.Request.Context(), status.Issue); environment != "" {
				props.Guidance, props.Environment = tachibanaGuidance(status.Issue, environment), environment
				props.Persistent = false
			}
		}
		if status.VersionRetiring {
			props.Notice = versionRetiringNotice
		}
	}
	if h.watchNotice != nil {
		if n := h.watchNotice.WatchNotice(c.Request.Context()); n != "" {
			props.Notice = strings.TrimSpace(props.Notice + " " + n)
		}
	}
	shared.RenderHTML(c, http.StatusOK, organisms.MarketDataBanner(props))
}
