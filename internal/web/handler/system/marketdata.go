package system

import (
	"context"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// versionRetiringNotice is the connection notice while the broker has announced
// an API release (立花 e支店, issue #727): not a failure, so the connection
// error stays out of the way of the real ones.
const versionRetiringNotice = "証券会社のAPIの更新（リリース予定日）が告知されています。変更内容を確認し、接続先URLの版数を更新してください。"

// MarketDataStatusSource is what MarketDataHandler needs from the broker
// adapter: the state of its session (broker.Session).
type MarketDataStatusSource interface {
	Status() broker.SessionStatus
}

// MarketDataHandler reads the broker connection's state for the Home screen's
// 要対応 list (issues #295, #739). A nil source (router-level tests) yields
// no condition.
type MarketDataHandler struct {
	source      MarketDataStatusSource
	selection   BrokerSelection
	watchNotice WatchNoticeSource
}

// WatchNoticeSource tells whether the 立花 watch list in use is a stand-in
// because the nightly daily bars could not be used (issue #730): the operator
// notice to show, or "" when the list is the planned one.
type WatchNoticeSource interface {
	WatchNotice(ctx context.Context) string
}

// WithWatchNotice adds the watch list fallback notice to BannerProps. It
// returns h for chaining.
func (h *MarketDataHandler) WithWatchNotice(source WatchNoticeSource) *MarketDataHandler {
	h.watchNotice = source
	return h
}

// NewMarketDataHandler returns a MarketDataHandler backed by source.
func NewMarketDataHandler(source MarketDataStatusSource) *MarketDataHandler {
	return &MarketDataHandler{source: source}
}

// WithBrokerSelection lets BannerProps follow the broker selection: with 立花
// selected it words the failure per cause and names the demo / production
// environment (issue #739). Without it (or with kabu selected) the content is
// the kabu one. It returns h for chaining.
func (h *MarketDataHandler) WithBrokerSelection(selection BrokerSelection) *MarketDataHandler {
	h.selection = selection
	return h
}

// BannerProps is the broker connection's content: the cause and remedy of the
// failing broker session (kabu: the last token issuance), its escalation and
// notices. The Home screen (issue #896) reads it for its 要対応 cards. With 立花
// selected the guidance is tachibanaGuidance's, Environment names the 立花
// environment and the kabu-specific escalation does not apply. A nil source
// yields the zero value (nothing to report).
func (h *MarketDataHandler) BannerProps(ctx context.Context) organisms.MarketDataBannerProps {
	var props organisms.MarketDataBannerProps
	if h.source != nil {
		status := h.source.Status()
		props.Issue, props.Guidance = string(status.Issue), status.Guidance
		props.Persistent, _ = status.Persistent(time.Now())
		if status.Failed() {
			if environment := h.tachibanaEnvironment(ctx, status.Issue); environment != "" {
				props.Guidance, props.Environment = tachibanaGuidance(status.Issue, environment), environment
				props.Persistent = false
			}
		}
		if status.VersionRetiring {
			props.Notice = versionRetiringNotice
		}
	}
	if h.watchNotice != nil {
		if n := h.watchNotice.WatchNotice(ctx); n != "" {
			props.Notice = strings.TrimSpace(props.Notice + " " + n)
		}
	}
	return props
}
