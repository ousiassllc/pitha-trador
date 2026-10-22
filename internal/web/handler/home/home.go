// Package home is the Home screen (`GET /`, issue #896): today's Paper P&L, the
// open positions and whether new entries are allowed (and, when not, why) on
// the top row; what needs the operator below it; the day's details under that.
// It is read-only: it gathers what the header banners, the Scanner, the
// Performance and the Settings screens already read and adds no write route.
// The Kill Switch buttons are the existing `pitha-kill-switch-panel`, which
// keeps calling the existing `/api/v1/system/*` API.
//
// Each card is its own fragment (`GET /home/<section>`) that htmx re-fetches
// every 30 s, so a read that fails takes down that card only, and the Kill
// Switch panel (a Lit element with a WebSocket) is never re-created by a swap.
// `GET /home/attention-badge` is the header's 「要対応 N 件」 badge on the
// other pages.
package home

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/homepanels"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// Deps are the Home screen's reads. Source is required; every other field may
// be nil (a build or a router-level test without that source): the card or
// item it feeds then shows nothing instead of failing.
type Deps struct {
	Source     Source
	State      StateReader
	DailyHalt  DailyHaltReader
	MarketData MarketDataReader
	Alerts     AlertReader
	Secrets    SecretsReader
	Update     UpdateReader
	Positions  PositionReader
	Scan       ScanReader
	Settings   SettingsReader
	Activity   ActivityReader
	// Now is the clock; nil is time.Now. Tests set it to reproduce the
	// trading-hours states.
	Now func() time.Time
}

// Handler implements the Home routes.
type Handler struct {
	deps Deps
	now  func() time.Time
}

// New returns a Handler reading from deps; a nil Source is StaticSource.
func New(deps Deps) *Handler {
	if deps.Source == nil {
		deps.Source = StaticSource{}
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{deps: deps, now: now}
}

// Register mounts `GET /` and the fragment routes on engine.
func (h *Handler) Register(engine *gin.Engine) {
	engine.GET("/", h.Page)
	engine.GET(homepanels.TradeStatusURL, h.TradeStatus)
	engine.GET(homepanels.PnLURL, h.PnL)
	engine.GET(homepanels.PositionsURL, h.Positions)
	engine.GET(homepanels.AttentionURL, h.Attention)
	engine.GET(homepanels.DetailsURL, h.Details)
	engine.GET(organisms.AttentionBadgeURL, h.AttentionBadge)
}

// Page implements `GET /`.
func (h *Handler) Page(c *gin.Context) {
	ctx := c.Request.Context()
	now := h.now()
	in := h.loadStatus(ctx, now)
	pos := h.loadPositions(ctx)
	view := homepanels.View{
		TradeStatus:   tradeStatus(in),
		PnL:           h.pnl(ctx, in, pos),
		Positions:     positions(pos, now),
		Attention:     attention(in),
		RejectReasons: h.rejectReasons(ctx, now),
		Scan:          h.scan(ctx),
		Proposals:     h.proposals(ctx, now),
		Trades:        h.trades(ctx, now),
		Model:         model(in),
		KillEvents:    h.killEvents(ctx),
	}
	shared.RenderHTML(c, http.StatusOK, pages.HomePage(view))
}

// TradeStatus implements `GET /home/trade-status`: the 取引の状態 card's body.
func (h *Handler) TradeStatus(c *gin.Context) {
	in := h.loadStatus(c.Request.Context(), h.now())
	shared.RenderHTML(c, http.StatusOK, homepanels.TradeStatusBody(tradeStatus(in)))
}

// PnL implements `GET /home/pnl`: the 今日の Paper 損益 card's body.
func (h *Handler) PnL(c *gin.Context) {
	ctx := c.Request.Context()
	in := h.loadStatus(ctx, h.now())
	shared.RenderHTML(c, http.StatusOK, homepanels.PnLBody(h.pnl(ctx, in, h.loadPositions(ctx))))
}

// Positions implements `GET /home/positions`: the 保有ポジション card's body.
func (h *Handler) Positions(c *gin.Context) {
	now := h.now()
	shared.RenderHTML(c, http.StatusOK, homepanels.PositionsBody(positions(h.loadPositions(c.Request.Context()), now)))
}

// Attention implements `GET /home/attention`: the 要対応 section's body.
func (h *Handler) Attention(c *gin.Context) {
	in := h.loadStatus(c.Request.Context(), h.now())
	shared.RenderHTML(c, http.StatusOK, homepanels.AttentionBody(attention(in)))
}

// Details implements `GET /home/details`: the 今日の詳細 section's body.
func (h *Handler) Details(c *gin.Context) {
	ctx := c.Request.Context()
	now := h.now()
	in := h.loadStatus(ctx, now)
	shared.RenderHTML(c, http.StatusOK, homepanels.DetailsBody(homepanels.View{
		RejectReasons: h.rejectReasons(ctx, now),
		Scan:          h.scan(ctx),
		Proposals:     h.proposals(ctx, now),
		Trades:        h.trades(ctx, now),
		Model:         model(in),
		KillEvents:    h.killEvents(ctx),
	}))
}

// AttentionBadge implements `GET /home/attention-badge`: the header's
// `#attention-badge` fragment, 「要対応 N 件」 linking to Home, or nothing while
// the list is empty. It counts exactly the items Home lists.
func (h *Handler) AttentionBadge(c *gin.Context) {
	in := h.loadStatus(c.Request.Context(), h.now())
	shared.RenderHTML(c, http.StatusOK, organisms.AttentionBadge(len(attention(in).Items)))
}
