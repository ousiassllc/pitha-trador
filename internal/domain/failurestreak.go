package domain

import "sync/atomic"

// FailureStreak counts consecutive failures of one external dependency
// (kabuステーションAPI, SQLite writes, ...). The dependency's own package
// records each outcome (Fail/Succeed); internal/service/risk.Engine polls
// ConsecutiveFailures against its threshold to raise the FR-RISK-2
// "一定回数継続" Kill Switch reasons (broker_api_error, db_write_failure).
// The zero value is ready to use and safe for concurrent use.
type FailureStreak struct {
	n atomic.Int64
}

// Fail records one more consecutive failure.
func (s *FailureStreak) Fail() { s.n.Add(1) }

// Succeed ends the current streak.
func (s *FailureStreak) Succeed() { s.n.Store(0) }

// ConsecutiveFailures returns how many failures have occurred since the
// last Succeed.
func (s *FailureStreak) ConsecutiveFailures() int { return int(s.n.Load()) }
