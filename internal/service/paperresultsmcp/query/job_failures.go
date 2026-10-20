package query

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/service/retention"
)

// lastErrorHeadBytes bounds how much of jobs.last_error one statement reads
// (the worker already truncates the column to 1 KiB). The text is parsed in
// the process and dropped: only a fixed code token, a queue, a JST day and
// counts are ever returned.
const lastErrorHeadBytes = 256

// skippedNotePrefix is how jobqueue records a terminal skip in last_error
// (internal/repository/jobqueue.Skip); such a job's status is succeeded.
const skippedNotePrefix = "skipped: "

// jobErrorCodeNone is the code of a failed job whose error carries no
// recognised code token.
const jobErrorCodeNone = "unknown"

// jobErrorCode finds the broker error code in the head of a failed job's
// error: kabu "code 4002006)", Tachibana "p_errno=-62" or "sResultCode=NNN".
// A code is an optional '-' and 1-8 digits; nothing else is kept.
var jobErrorCode = regexp.MustCompile(`(?:\bcode |p_errno=|sResultCode=)(-?[0-9]{1,8})\b`)

// RetentionNote documents the retention that bounds every job count of
// collection_status (internal/service/retention): finished jobs older than
// these windows are deleted, so a period reaching further back sees the
// failures but not the successes.
type RetentionNote struct {
	SucceededJobDays int    `json:"succeeded_job_days"`
	FailedJobDays    int    `json:"failed_job_days"`
	Note             string `json:"note"`
}

// DayCount, QueueCount, CodeCount and DayCodeCount are the failure counts of
// JobFailures.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type QueueCount struct {
	Queue string `json:"queue"`
	Count int    `json:"count"`
}

type CodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type DayCodeCount struct {
	Date  string `json:"date"`
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// JobFailures are the failed jobs finished in the period by JST day, queue and
// error code (each list sums to the same total), and the jobs that ended as a
// terminal skip (status succeeded with a "skipped:" note) by queue. The error
// code is a token parsed out of jobs.last_error; the text itself is never
// returned.
type JobFailures struct {
	Total        int            `json:"total"`
	ByDay        []DayCount     `json:"by_day"`
	ByQueue      []QueueCount   `json:"by_queue"`
	ByErrorCode  []CodeCount    `json:"by_error_code"`
	ByDayAndCode []DayCodeCount `json:"by_day_and_error_code"`
	// SkippedTotal / SkippedByQueue count the terminal skips (a subset of the
	// succeeded counts of jobs[]).
	SkippedTotal   int          `json:"skipped_total"`
	SkippedByQueue []QueueCount `json:"skipped_by_queue"`
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

// jobFailures reads the failed and skipped jobs finished in [from, to). The
// statement groups by queue, JST day, status and the bounded head of the
// error, so the rows read are the distinct messages, not the jobs.
func jobFailures(ctx context.Context, q Q, from, to string) (JobFailures, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
SELECT queue, status, substr(datetime(finished_at, '+9 hours'), 1, 10),
	CASE WHEN status = 'failed' THEN substr(last_error, 1, %d) ELSE substr(last_error, 1, %d) END, COUNT(*)
FROM jobs
WHERE status IN ('succeeded', 'failed') AND finished_at >= ? AND finished_at < ?
	AND (status = 'failed' OR substr(last_error, 1, %d) = '%s')
GROUP BY 1, 2, 3, 4`, lastErrorHeadBytes, len(skippedNotePrefix), len(skippedNotePrefix), skippedNotePrefix), from, to)
	if err != nil {
		return JobFailures{}, fmt.Errorf("query: job failures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byDay, byQueue, byCode, byDayCode, skipped := map[string]int{}, map[string]int{}, map[string]int{}, map[[2]string]int{}, map[string]int{}
	var out JobFailures
	for rows.Next() {
		var (
			queue, status, day string
			head               *string
			n                  int
		)
		if err := rows.Scan(&queue, &status, &day, &head, &n); err != nil {
			return JobFailures{}, fmt.Errorf("query: scan job failures: %w", err)
		}
		if status == "succeeded" {
			skipped[queue] += n
			out.SkippedTotal += n
			continue
		}
		code := jobErrorCodeNone
		if head != nil {
			if m := jobErrorCode.FindStringSubmatch(*head); m != nil {
				code = m[1]
			}
		}
		out.Total += n
		byDay[day] += n
		byQueue[queue] += n
		byCode[code] += n
		byDayCode[[2]string{day, code}] += n
	}
	if err := rows.Err(); err != nil {
		return JobFailures{}, fmt.Errorf("query: job failures: %w", err)
	}

	out.ByDay, out.ByQueue, out.ByErrorCode, out.ByDayAndCode, out.SkippedByQueue =
		[]DayCount{}, []QueueCount{}, []CodeCount{}, []DayCodeCount{}, []QueueCount{}
	for d, n := range byDay {
		out.ByDay = append(out.ByDay, DayCount{Date: d, Count: n})
	}
	sort.Slice(out.ByDay, func(i, j int) bool { return out.ByDay[i].Date < out.ByDay[j].Date })
	out.ByQueue = queueCounts(byQueue)
	out.SkippedByQueue = queueCounts(skipped)
	for c, n := range byCode {
		out.ByErrorCode = append(out.ByErrorCode, CodeCount{Code: c, Count: n})
	}
	sort.Slice(out.ByErrorCode, func(i, j int) bool {
		a, b := out.ByErrorCode[i], out.ByErrorCode[j]
		return a.Count > b.Count || (a.Count == b.Count && a.Code < b.Code)
	})
	for k, n := range byDayCode {
		out.ByDayAndCode = append(out.ByDayAndCode, DayCodeCount{Date: k[0], Code: k[1], Count: n})
	}
	sort.Slice(out.ByDayAndCode, func(i, j int) bool {
		a, b := out.ByDayAndCode[i], out.ByDayAndCode[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.Count > b.Count || (a.Count == b.Count && a.Code < b.Code)
	})
	return out, nil
}

// queueCounts lists counts in the order of Queues; a queue name outside it
// (never written by the scheduler) is not reported by name but as "other".
func queueCounts(counts map[string]int) []QueueCount {
	out := []QueueCount{}
	known := map[string]bool{}
	for _, name := range Queues {
		known[name] = true
		if n := counts[name]; n > 0 {
			out = append(out, QueueCount{Queue: name, Count: n})
		}
	}
	other := 0
	for name, n := range counts {
		if !known[name] {
			other += n
		}
	}
	if other > 0 {
		out = append(out, QueueCount{Queue: "other", Count: other})
	}
	return out
}
