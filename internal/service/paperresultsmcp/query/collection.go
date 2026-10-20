package query

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Queues are the job queues collection_status reports, in output order.
var Queues = []string{"market-data", "feature-calc", "jev-scout", "jev-trader", "outcome-labeling", "analytics"}

// JobStatus is one jobs[] element of collection_status (API spec §3.8).
// payload_json is not selected, and last_error is read only in JobFailures'
// bounded head, never returned.
type JobStatus struct {
	Queue                     string  `json:"queue"`
	SucceededCount            int     `json:"succeeded_count"`
	FailedCount               int     `json:"failed_count"`
	LatestSucceededFinishedAt *string `json:"latest_succeeded_finished_at"`
}

// Collection is collection_status' data.
type Collection struct {
	LatestSnapshotAt       *string     `json:"latest_snapshot_at"`
	MissingSessionDates    []string    `json:"missing_session_dates"`
	Jobs                   []JobStatus `json:"jobs"`
	DailyBarRunFailedCount int         `json:"daily_bar_run_failed_count"`
	LabelSkipCount         int         `json:"label_skip_count"`
	// JobFailures breaks the failed jobs of the period down by JST day, queue
	// and error code and counts the terminal skips (issue #801).
	JobFailures JobFailures `json:"job_failures"`
	// Alerts are the health alerts active now (issue #802): fixed codes
	// (AlertKabuNotLoggedIn, AlertMarketDataIdle) with when they began.
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
	if c.JobFailures, err = jobFailures(ctx, q, from, to); err != nil {
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
