package scheduler

import "time"

// WithSessionGate restricts the market-data-driven triggers to trading
// sessions (non-functional.md §3: 立会時間外は市場データ取得・Jev呼び出し・
// 新規発注を停止し、Scheduler/Workerは待機状態に入る). open reports
// whether t is inside a session (internal/service/marketcalendar's
// Calendar.IsOpen; passed in as a plain func to keep this package free of
// other service imports, doc.go). While it reports false, EnqueueFullScan
// and EnqueueEventReevaluation enqueue nothing. Unset by default: every
// trigger then runs unconditionally.
//
// It deliberately does not gate the maintenance jobs (backup, purge, log
// rotation - maintenance.go) or outcome labeling, which work on data
// already stored and are meant to run outside session hours.
func WithSessionGate(open func(t time.Time) bool) Option {
	return func(s *Scheduler) { s.sessionOpen = open }
}

// inSession reports whether the session-gated triggers may run at now.
func (s *Scheduler) inSession(now time.Time) bool {
	return s.sessionOpen == nil || s.sessionOpen(now)
}
