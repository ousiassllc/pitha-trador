package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/jobfailures"
	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

// Queues are the job queues collection_status reports, in output order.
var Queues = []string{"market-data", "feature-calc", "jev-scout", "jev-trader", "outcome-labeling", "analytics"}

// retiredQueues are the queues no producer feeds any more, with why zero
// counts are expected. feature-calc: marketdatajob.HandleMarketData computes
// and persists the features itself and EnqueueFullScan no longer feeds it.
var retiredQueues = map[string]string{
	"feature-calc": "retired queue: market-data computes and persists features; nothing enqueues feature-calc, so 0 succeeded and 0 failed is expected, not a collection problem",
}

// JobStatus is one jobs[] element of collection_status (API spec §3.8).
// payload_json is not selected, and last_error is read only in JobFailures'
// bounded head, never returned.
type JobStatus struct {
	Queue                     string  `json:"queue"`
	SucceededCount            int     `json:"succeeded_count"`
	FailedCount               int     `json:"failed_count"`
	LatestSucceededFinishedAt *string `json:"latest_succeeded_finished_at"`
	// Retired marks a queue nothing feeds any more (zero counts are
	// expected); RetiredNote says why, and is empty otherwise.
	Retired     bool   `json:"retired"`
	RetiredNote string `json:"retired_note"`
}

// Collection is collection_status' data.
type Collection struct {
	LatestSnapshotAt       *string     `json:"latest_snapshot_at"`
	MissingSessionDates    []string    `json:"missing_session_dates"`
	Jobs                   []JobStatus `json:"jobs"`
	DailyBarRunFailedCount int         `json:"daily_bar_run_failed_count"`
	LabelSkipCount         int         `json:"label_skip_count"`
	// JobFailures breaks the failed jobs down by JST day, queue and error code
	// and counts the terminal skips (issue #801). It covers at most the last
	// jobfailures.MaxDays days of the period (Window says which; issue #850).
	JobFailures jobfailures.Result `json:"job_failures"`
	// Alerts are the health alerts active now (issue #802): fixed codes
	// (AlertKabuNotLoggedIn, AlertKabuUnreachable, AlertMarketDataIdle) with when they began.
	// They describe the present, not the period.
	Alerts []Alert `json:"alerts"`
	// Retention documents how long finished jobs are kept, which bounds every
	// job count above.
	Retention RetentionNote `json:"retention"`
}

// CollectionStatus reads the collection health of r, and the alerts active at
// now. market_snapshots is touched only for MAX(timestamp) and existence
// checks per trading day (both served by market_snapshots_timestamp_idx); no
// price or raw column is read.
func CollectionStatus(ctx context.Context, q Q, r Range, now time.Time) (Collection, error) {
	from, to := r.bounds()
	c := Collection{MissingSessionDates: []string{}, Jobs: []JobStatus{}}

	var latest sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT MAX(timestamp) FROM market_snapshots`).Scan(&latest); err != nil {
		return Collection{}, fmt.Errorf("query: latest snapshot: %w", err)
	}
	c.LatestSnapshotAt = formatOutPtr(latest)

	var err error
	if c.MissingSessionDates, err = missingSessionDates(ctx, q, r); err != nil {
		return Collection{}, err
	}

	for _, queue := range Queues {
		j := JobStatus{Queue: queue}
		j.RetiredNote, j.Retired = retiredQueues[queue]
		var lastOK sql.NullString
		err := q.QueryRowContext(ctx, `
SELECT COALESCE(SUM(status = 'succeeded'), 0), COALESCE(SUM(status = 'failed'), 0)
FROM jobs WHERE queue = ? AND status IN ('succeeded', 'failed') AND finished_at >= ? AND finished_at < ?`,
			queue, from, to).Scan(&j.SucceededCount, &j.FailedCount)
		if err != nil {
			return Collection{}, fmt.Errorf("query: job counts: %w", err)
		}
		err = q.QueryRowContext(ctx, `
SELECT MAX(finished_at) FROM jobs WHERE queue = ? AND status = 'succeeded'`, queue).Scan(&lastOK)
		if err != nil {
			return Collection{}, fmt.Errorf("query: latest job: %w", err)
		}
		j.LatestSucceededFinishedAt = formatOutPtr(lastOK)
		c.Jobs = append(c.Jobs, j)
	}

	fromDate, toDate := r.From.In(JST).Format(time.DateOnly), r.To.In(JST).Format(time.DateOnly)
	err = q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM daily_bar_runs WHERE status = 'failed' AND run_date >= ? AND run_date < ?`,
		fromDate, toDate).Scan(&c.DailyBarRunFailedCount)
	if err != nil {
		return Collection{}, fmt.Errorf("query: daily bar runs: %w", err)
	}
	err = q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM calibration_label_skips WHERE created_at >= ? AND created_at < ?`,
		from, to).Scan(&c.LabelSkipCount)
	if err != nil {
		return Collection{}, fmt.Errorf("query: label skips: %w", err)
	}
	if c.JobFailures, err = jobfailures.Read(ctx, q, Queues, r.From, r.To); err != nil {
		return Collection{}, err
	}
	if c.Alerts, err = HealthAlerts(ctx, q, now); err != nil {
		return Collection{}, err
	}
	c.Retention = retentionNote()
	return c, nil
}

// missingSessionDates lists the TSE trading days (JST) of r with no
// market_snapshots row.
func missingSessionDates(ctx context.Context, q Q, r Range) ([]string, error) {
	missing := []string{}
	for day := r.From.In(JST); day.Before(r.To); day = day.AddDate(0, 0, 1) {
		if !marketcalendar.TSE.IsTradingDay(day) {
			continue
		}
		exists, err := snapshotExists(ctx, q, day, day.AddDate(0, 0, 1))
		if err != nil {
			return nil, err
		}
		if !exists {
			missing = append(missing, day.Format(time.DateOnly))
		}
	}
	if len(missing) > MaxRows {
		missing = missing[:MaxRows]
	}
	return missing, nil
}

// RetentionNote documents the retention that bounds every job count of
// collection_status (internal/service/retention): finished jobs older than
// these windows are deleted, so a period reaching further back sees the
// failures but not the successes.
type RetentionNote struct {
	SucceededJobDays int    `json:"succeeded_job_days"`
	FailedJobDays    int    `json:"failed_job_days"`
	Note             string `json:"note"`
}

// retentionNote returns the retention windows in force.
func retentionNote() RetentionNote {
	return RetentionNote{
		SucceededJobDays: retention.DefaultSucceededJobDays,
		FailedJobDays:    retention.DefaultFailedJobDays,
		Note: "finished jobs are deleted after succeeded_job_days (succeeded) and failed_job_days (failed); " +
			"a period reaching further back than that no longer sees those jobs, and succeeded counts disappear earlier than failed ones",
	}
}
