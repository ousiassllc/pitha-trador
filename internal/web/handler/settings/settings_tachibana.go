package settings

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings/tachibanapanel"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// tachibanaConnectionID is the 立花証券 e支店 connection (settingsConnections).
const tachibanaConnectionID = "tachibana"

// BrokerSession is the running broker adapter as the 立花証券 e支店 card
// sees it (issue #738): its identity and the state of its login session. The
// neutral broker.Broker satisfies it.
type BrokerSession interface {
	Capabilities() broker.Capabilities
	Status() broker.SessionStatus
}

// WithBrokerSession lets the 立花 card show the adapter's session state. It
// is shown only while the running adapter really is 立花 (a saved selection
// takes effect after a restart, and kabu runs until then).
func (h *SettingsHandler) WithBrokerSession(session BrokerSession) *SettingsHandler {
	h.session = session
	return h
}

// tachibanaStatus builds the 立花 card's badges and detail lines, secrets
// excluded: the 接続環境 (デモ / 本番, always shown, with the no-orders
// notice for 本番), whether 立花 is the selected broker, and - while the 立花
// adapter runs - the session's last login, next re-login, API version and the
// broker's notices. Credentials, key paths, URLs and Guidance text never
// appear.
func (h *SettingsHandler) tachibanaStatus(ctx context.Context) ([]molecules.ConnectionBadge, []molecules.ConnectionDetail) {
	b := brokerSettings(ctx, h.ops)
	var badges []molecules.ConnectionBadge
	if b.Tachibana.Production() {
		badges = append(badges,
			molecules.ConnectionBadge{ID: "environment", Text: "本番環境", Tone: molecules.BadgeDanger},
			molecules.ConnectionBadge{ID: "no-orders", Text: "発注は行いません", Tone: molecules.BadgeWarning})
	} else {
		badges = append(badges, molecules.ConnectionBadge{ID: "environment", Text: "デモ環境", Tone: molecules.BadgeInfo})
	}
	if b.Provider == config.BrokerTachibana {
		badges = append(badges, molecules.ConnectionBadge{ID: "selected", Text: "使用するブローカー", Tone: molecules.BadgeInfo})
	}
	if h.session == nil || h.session.Capabilities().Name != config.BrokerTachibana {
		return badges, nil
	}
	return badges, tachibanapanel.SessionDetails(h.session.Status())
}
