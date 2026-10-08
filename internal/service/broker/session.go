package broker

import "time"

// SessionIssue classifies why the broker session is not usable, so the UI
// can tell the operator what to fix (issue #295) instead of one generic
// error. SessionIssueNone means the session is fine (or none was attempted
// yet). The adapter maps its own failure causes onto these.
type SessionIssue string

const (
	SessionIssueNone        SessionIssue = ""
	SessionIssueUnreachable SessionIssue = "unreachable"   // 接続できない: 常駐アプリ未起動 / API未有効
	SessionIssueNotLoggedIn SessionIssue = "not_logged_in" // 人手ログインが必要 / セッション切れ
	SessionIssueAPIDisabled SessionIssue = "api_disabled"  // API利用設定が未完了
	SessionIssueBadPassword SessionIssue = "bad_password"  // 認証情報が不正
	SessionIssueUnknown     SessionIssue = "unknown"       // 上記以外
	SessionIssueRejected    SessionIssue = "rejected"      // 認証は成功するが以降の要求が拒否される
)

// A not_logged_in failure streak is "persistent" (issue #712) once it has
// repeated this many times or lasted this long: the broker still wants a
// login well after the app's own retries, so the operator has to log in by
// hand (post-maintenance morning routine) and the banner says so
// prominently.
const (
	persistentFailures = 5
	persistentElapsed  = 5 * time.Minute
)

// SessionStatus is a snapshot of the broker session's state (Session.Status).
// Issue is the cause; Code is the broker-specific error code when the failure
// carried one (0 otherwise); Guidance is the adapter's operator-facing text
// on what to check ("" when nothing failed). Failures and Since describe the
// current streak of consecutive failures with the same Issue (Since is when
// it began).
type SessionStatus struct {
	Issue    SessionIssue
	Code     int
	Guidance string
	Failures int
	Since    time.Time
}

// Failed reports whether the session is currently failing.
func (s SessionStatus) Failed() bool { return s.Issue != SessionIssueNone }

// Persistent reports whether a not_logged_in streak has repeated or lasted
// long enough that the UI should escalate beyond the ordinary banner (issue
// #712), and how long it has lasted at now.
func (s SessionStatus) Persistent(now time.Time) (bool, time.Duration) {
	if s.Issue != SessionIssueNotLoggedIn || s.Since.IsZero() {
		return false, 0
	}
	elapsed := max(now.Sub(s.Since), 0)
	return s.Failures >= persistentFailures || elapsed >= persistentElapsed, elapsed
}
