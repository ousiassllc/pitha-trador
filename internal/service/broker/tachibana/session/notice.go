package session

import (
	"context"
	"log/slog"
)

// NoticeKind classifies an operator notice of the adapter.
type NoticeKind string

const (
	// NoticeLoginOverdue: no login succeeded by 08:30 (the morning re-login
	// failed through its backoff retries).
	NoticeLoginOverdue NoticeKind = "login_overdue"
	// NoticeContention: the session keeps being cut right after a login
	// (another process or tool logs in with the same 認証ID).
	NoticeContention NoticeKind = "contention"
	// NoticeDocumentsUnread: the broker accepted the login but issued no
	// virtual URLs because 書面 are unread.
	NoticeDocumentsUnread NoticeKind = "documents_unread"
	// NoticeAPISpecUpdate: a new API release date was announced
	// (sUpdateInformAPISpecFunction changed to today or later).
	NoticeAPISpecUpdate NoticeKind = "api_spec_update"
	// NoticeDocumentUpdate: a new 交付書面 update date was announced
	// (sUpdateInformWebDocument changed to today or later).
	NoticeDocumentUpdate NoticeKind = "document_update"
)

// Notice is one message for the operator. Message is safe to show and log:
// it never carries a credential or a virtual URL.
type Notice struct {
	Kind    NoticeKind
	Message string
}

// Notifier delivers notices beyond the WARN log the adapter writes itself
// (bootstrap wires the Activity feed and Slack). It must not block for long
// and its failures are the notifier's to log.
type Notifier interface {
	Notify(ctx context.Context, n Notice)
}

// NotifierFunc adapts a function to a Notifier.
type NotifierFunc func(ctx context.Context, n Notice)

func (f NotifierFunc) Notify(ctx context.Context, n Notice) { f(ctx, n) }

// notify logs n at WARN and hands it to the notifier, if any.
func notify(ctx context.Context, nf Notifier, n Notice) {
	slog.Warn("tachibana: operator notice", "kind", string(n.Kind), "message", n.Message)
	if nf != nil {
		nf.Notify(ctx, n)
	}
}
