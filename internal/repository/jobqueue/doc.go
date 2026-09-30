// Package jobqueue persists the jobs table used by the Job Queue /
// Scheduler: the Job type, the queue-name and status constants, and
// JobRepository (job_repo.go) with its activity-feed queries
// (job_queries.go).
//
// It MUST depend only on internal/domain and internal/repository/sqlutil.
// See docs/architecture/overview.md §3.
package jobqueue
