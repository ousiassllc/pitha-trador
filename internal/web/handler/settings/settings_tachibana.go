package settings

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
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
			molecules.ConnectionBadge{ID: "no-orders", Text: "発注は行いません（#55 まで）", Tone: molecules.BadgeWarning})
	} else {
		badges = append(badges, molecules.ConnectionBadge{ID: "environment", Text: "デモ環境", Tone: molecules.BadgeInfo})
	}
	if b.Provider == config.BrokerTachibana {
		badges = append(badges, molecules.ConnectionBadge{ID: "selected", Text: "使用するブローカー", Tone: molecules.BadgeInfo})
	}
	if h.session == nil || h.session.Capabilities().Name != config.BrokerTachibana {
		return badges, nil
	}
	return badges, sessionDetails(h.session.Status())
}

func sessionDetails(s broker.SessionStatus) []molecules.ConnectionDetail {
	version := s.APIVersion
	if version == "" {
		version = "不明"
	}
	details := []molecules.ConnectionDetail{
		{ID: "last-login", Label: "最終ログイン", Value: timeOrDash(s.LoggedInAt)},
		{ID: "next-reauth", Label: "次回再認証予定", Value: timeOrDash(s.NextReauth)},
		{ID: "api-version", Label: "API 版数", Value: version},
	}
	if s.DocumentsUnread {
		details = append(details, molecules.ConnectionDetail{ID: "documents-unread", Label: "お知らせ", Value: "立花証券の書面が未読です。標準Webで確認してください"})
	}
	if s.VersionRetiring {
		details = append(details, molecules.ConnectionDetail{ID: "version-retiring", Label: "お知らせ", Value: "API の版数更新が予告されています。接続先 URL の版数を更新してください"})
	}
	return details
}

func timeOrDash(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return atoms.FormatJST(t)
}
