// Package scheduler is the self-hosted worker pool + periodic trigger
// that stands in for River (docs/architecture/overview.md §2 "Job Queue /
// Scheduler", §4.10 Scheduler/Worker, functional.md §4.10 FR-SCHED-1〜6).
//
// Scheduler claims jobs.Job rows off each registered queue and invokes the
// Handler registered for that queue (the market-data and the feature-calc
// no-op compat queue are registered by internal/bootstrap/marketdatajob;
// the full scan never feeds feature-calc, FR-SCHED-1), drives the
// 60-second full-scan cycle that enqueues
// market-data work for every active instrument in one transaction
// (FR-SCHED-2 前半), optionally drives Outcome Labeling's periodic
// enqueue trigger (EnqueueOutcomeLabeling, WithOutcomeLabelSource,
// functional.md FR-CAL-4), enqueues an immediate jev-scout job bypassing
// that cadence when a caller reports an instrument's
// eventtrigger.Signal fired (EnqueueEventReevaluation,
// functional.md FR-SCAN-1/FR-SCAN-2), optionally drives the operator
// heartbeat dead-man's-switch periodic check (CheckOperatorHeartbeat,
// WithHeartbeatChecker, functional.md FR-RISK-6), fails jobs left
// status='running' on any queue for over 10 minutes (orphans.FailAll,
// registered as a 1-minute periodic trigger, non-functional.md §2.1),
// and recovers jobs left status='running' by a previous crash back to
// pending at startup (er.md §jobs).
//
// This package MUST depend only on internal/domain and internal/repository
// (internal/service/doc.go); it does not import internal/service/marketdata
// or internal/service/featureengine (job processing is wired in by the
// caller via RegisterHandler) or internal/config (periodic intervals are
// passed in as plain time.Duration values).
package scheduler
