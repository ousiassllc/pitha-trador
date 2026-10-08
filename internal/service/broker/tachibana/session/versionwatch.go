package session

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// observeNotices compares the login response's announced dates with the
// previous login's, as the manual's 【注意２】 prescribes: a date that is today
// or later AND differs from the last received value (empty on the first
// login) is news. The API release date (sUpdateInformAPISpecFunction) means
// the interface or version may change (a retiring version is dropped about
// 30 days after its successor ships); the document date
// (sUpdateInformWebDocument) means 書面 must be confirmed on the 標準Web by
// then. Each raises a WARN log, a notice (Activity, Slack) and
// SessionStatus.VersionRetiring while an announced date is pending.
func (s *Session) observeNotices(ctx context.Context, info tachibana.LoginInfo) {
	if info == (tachibana.LoginInfo{}) { // no login response was read
		return
	}
	today := tachibana.DayKey(s.clock.Now())
	pending := func(date string) bool { return validDay(date) && date >= today }

	if pending(info.APISpecUpdate) && info.APISpecUpdate != s.prevSpec {
		notify(ctx, s.cfg.Notifier, Notice{Kind: NoticeAPISpecUpdate, Message: fmt.Sprintf(
			"立花証券 e支店・APIのリリース予定日（%s）が告知されました。専用ページの「リリース＆改定情報」で変更内容（インタフェースの変更・旧版の廃止）を確認し、予定日までに対応してください。",
			displayDay(info.APISpecUpdate))})
	}
	if pending(info.DocumentUpdate) && info.DocumentUpdate != s.prevDocUpdate {
		notify(ctx, s.cfg.Notifier, Notice{Kind: NoticeDocumentUpdate, Message: fmt.Sprintf(
			"立花証券の交付書面の更新予定日（%s）が告知されました。予定日までに標準Webで書面を確認してください（未読になるとAPIを使えません）。",
			displayDay(info.DocumentUpdate))})
	}
	s.prevSpec, s.prevDocUpdate = info.APISpecUpdate, info.DocumentUpdate

	s.mu.Lock()
	s.status.VersionRetiring = pending(info.APISpecUpdate)
	s.mu.Unlock()
}

// validDay reports whether s is a YYYYMMDD date.
func validDay(s string) bool {
	if len(s) != 8 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func displayDay(s string) string { return s[:4] + "-" + s[4:6] + "-" + s[6:] }
