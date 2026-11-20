// Package tachibanapanel renders the 立花証券 e支店 card's session detail lines of the Settings screen
// (issue #738) from the broker's session status.
package tachibanapanel

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/molecules"
)

// SessionDetails is the 立花 card's session lines: last login, next re-login, API version and the
// broker's notices. Secrets, URLs and Guidance text never appear.
func SessionDetails(s broker.SessionStatus) []molecules.ConnectionDetail {
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
