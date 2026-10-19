// Package jobfailurelog writes the jobs that failed since the previous tick
// to the error log, aggregated by error code (issue #803). A failed job's
// cause otherwise lives only in jobs.last_error, which is purged after 30
// days and needs the database to read; here each tick logs, per error code,
// how many jobs failed, on which queues, and one masked representative
// message, so the cause is readable from the error log alone.
//
// Only the jobs that finished failing since the last tick are reported (a
// persistent cursor over (finished_at, id)), so nothing is logged twice and
// the output per tick is bounded by maxCodes, whatever the failure volume.
package jobfailurelog

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/safego"
)

const (
	// Interval is how often failures are aggregated and logged.
	Interval = time.Minute
	// Grace holds the upper end of each window back from the tick time: the
	// scheduler stamps finished_at before its completion write, which can wait
	// up to 5s (scheduler.recordTimeout) on a busy database, so a failure
	// stamped just before a tick can still be uncommitted when it queries.
	Grace = 10 * time.Second

	pageSize = 500
	// maxPagesPerTick bounds one tick's reading (maxPagesPerTick*pageSize
	// rows); a backlog beyond it resumes on the next tick.
	maxPagesPerTick = 100
	// maxCodes bounds the number of per-code records one tick logs; the rest
	// are summed in one trailing record.
	maxCodes = 20
)

// Source is the jobqueue.JobRepository method the reporter reads failures with.
type Source interface {
	ListFailedAfter(ctx context.Context, cursor jobqueue.FailedCursor, upTo time.Time, limit int) ([]jobqueue.FailedJob, error)
}

// Reporter aggregates newly failed jobs by error code and logs them. It is
// driven by one goroutine (Run / RunEvery) and is not safe for concurrent use.
type Reporter struct {
	source Source
	cursor jobqueue.FailedCursor
}

// New returns a Reporter that reports the failures that finished after
// start-Grace, i.e. it does not replay the retained history of earlier runs.
func New(source Source, start time.Time) *Reporter {
	return &Reporter{source: source, cursor: jobqueue.FailedCursorAtTime(start.Add(-Grace))}
}

// group is the failures of one error code within one tick.
type group struct {
	code   string
	count  int
	queues map[string]int
	// first is the oldest failure of the group, whose message represents it.
	first jobqueue.FailedJob
}

// Report reads the failures that finished in (cursor, now-Grace], logs one
// ERROR record per error code (most frequent first) and moves the cursor to
// the end of what it read. A read error leaves the cursor in place and logs
// nothing, so the next tick covers the same failures again.
func (r *Reporter) Report(ctx context.Context, now time.Time) error {
	upTo := now.UTC().Add(-Grace)
	cursor := r.cursor
	groups := map[string]*group{}
	exhausted := false
	for page := 0; page < maxPagesPerTick && !exhausted; page++ {
		jobs, err := r.source.ListFailedAfter(ctx, cursor, upTo, pageSize)
		if err != nil {
			return fmt.Errorf("jobfailurelog: read failed jobs: %w", err)
		}
		for _, j := range jobs {
			addToGroup(groups, j)
		}
		if len(jobs) > 0 {
			cursor = jobs[len(jobs)-1].Cursor()
		}
		exhausted = len(jobs) < pageSize
	}
	windowStart := r.cursor.FinishedAt
	if exhausted && upTo.After(cursor.FinishedAt) {
		cursor = jobqueue.FailedCursorAtTime(upTo)
	}
	r.cursor = cursor
	logGroups(groups, windowStart, cursor.FinishedAt, !exhausted)
	return nil
}

func addToGroup(groups map[string]*group, j jobqueue.FailedJob) {
	code := Classify(j.LastError)
	g := groups[code]
	if g == nil {
		g = &group{code: code, queues: map[string]int{}, first: j}
		groups[code] = g
	}
	g.count++
	g.queues[j.Queue]++
}

func logGroups(groups map[string]*group, from, to time.Time, truncated bool) {
	if len(groups) == 0 {
		return
	}
	sorted := make([]*group, 0, len(groups))
	for _, g := range groups {
		sorted = append(sorted, g)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].code < sorted[j].code
	})

	window := []any{"window_start", from.UTC().Format(time.RFC3339), "window_end", to.UTC().Format(time.RFC3339)}
	omittedCodes, omittedJobs := 0, 0
	for i, g := range sorted {
		if i >= maxCodes {
			omittedCodes++
			omittedJobs += g.count
			continue
		}
		slog.Error("jobfailurelog: jobs failed", append([]any{
			"error_code", g.code,
			"count", g.count,
			"queues", formatQueues(g.queues),
			"message", maskMessage(g.first.LastError),
			"message_queue", g.first.Queue,
			"message_job_id", g.first.ID,
		}, window...)...)
	}
	if omittedCodes > 0 || truncated {
		slog.Error("jobfailurelog: more failed jobs than listed",
			append([]any{"omitted_error_codes", omittedCodes, "omitted_count", omittedJobs, "read_truncated", truncated}, window...)...)
	}
}

// formatQueues renders per-queue counts as "queue=n,queue=n" in queue-name
// order (at most one entry per job queue).
func formatQueues(queues map[string]int) string {
	names := make([]string, 0, len(queues))
	for q := range queues {
		names = append(names, q)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, q := range names {
		parts[i] = q + "=" + strconv.Itoa(queues[q])
	}
	return strings.Join(parts, ",")
}

// Run reports on every value received from ticks until ctx is done. A failed
// or panicking tick is logged and the next one still runs.
func (r *Reporter) Run(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticks:
			safego.Run("job failure log", func() {
				if err := r.Report(ctx, now); err != nil && ctx.Err() == nil {
					slog.Error("jobfailurelog: tick failed", "error", err)
				}
			})
		}
	}
}

// Start launches the once-a-minute failure summary on wg: it reports the
// failures that finish from now on, until ctx is done.
func Start(ctx context.Context, wg *sync.WaitGroup, source Source) {
	reporter := New(source, time.Now())
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(Interval)
		defer t.Stop()
		reporter.Run(ctx, t.C)
	}()
}
