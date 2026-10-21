// Package jobfailures is the failed-job breakdown of collection_status
// (issues #801, #850): by JST day, queue and broker error code, from a bounded
// window and without ever returning job text.
package jobfailures

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// lastErrorHeadBytes bounds how much of jobs.last_error one statement reads
// (the worker already truncates the column to 1 KiB). The head is reduced to
// a code token inside SQL (errorCodeSQL) and dropped: only a fixed code
// token, a queue, a JST day and counts are ever returned.
const lastErrorHeadBytes = 256

// MaxDays is the longest span of job_failures: the failed-job
// breakdown covers at most this many JST days ending at the period's end, so
// that a 30- or 366-day collection_status does not scan a failure burst of
// weeks ago (issue #850). It is also the default, as the default 30-day
// period is cut to the same span. The value is the issue's "default 7 days";
// whether it is the right ceiling is still to be confirmed.
const MaxDays = 7

// skippedNotePrefix is how jobqueue records a terminal skip in last_error
// (internal/repository/jobqueue.Skip); such a job's status is succeeded.
const skippedNotePrefix = "skipped: "

// jobErrorCodeNone is the code of a failed job whose error carries no
// recognised code token.
const jobErrorCodeNone = "unknown"

// jobErrorCodeToken is what a code token must look like once SQL has cut it
// out of the error: an optional '-' and 1-8 digits; nothing else is kept.
var jobErrorCodeToken = regexp.MustCompile(`^-?[0-9]{1,8}$`)

// errorCodePrefixes are the markers a broker error code follows, in the
// order they are tried: kabu "(http 429, code 4001006)", Tachibana
// "p_errno=-62" and "sResultCode=NNN".
var errorCodePrefixes = []string{"code ", "p_errno=", "sResultCode="}

// codeDigits is the SQL character set of a code token; ltrim strips the run
// of them from the front of the text after a marker, which leaves the token.
const codeDigits = "-0123456789"

// errorCodeSQL is a SELECT over jobs rows of the period: for each, the JST
// day and the code token found in the bounded head of last_error (empty when
// none), with no free text left, so the rows group by (day, code) into a
// handful of groups instead of one group per message (the message names the
// symbol). where is the WHERE condition, with its own placeholders.
func errorCodeSQL(where string) string {
	var window, digits, pick []string
	for i, p := range errorCodePrefixes {
		w, d := fmt.Sprintf("w%d", i+1), fmt.Sprintf("d%d", i+1)
		window = append(window, fmt.Sprintf(
			"CASE WHEN instr(h, '%[1]s') > 0 THEN substr(h, instr(h, '%[1]s') + %[2]d, 10) ELSE '' END AS %[3]s", p, len(p), w))
		digits = append(digits, fmt.Sprintf("substr(%[1]s, 1, length(%[1]s) - length(ltrim(%[1]s, '%[2]s'))) AS %[3]s", w, codeDigits, d))
		pick = append(pick, fmt.Sprintf("NULLIF(%s, '')", d))
	}
	return fmt.Sprintf(`
SELECT day, COALESCE(%s, '') AS code FROM (
	SELECT day, %s FROM (
		SELECT day, %s FROM (
			SELECT substr(datetime(finished_at, '+9 hours'), 1, 10) AS day, substr(last_error, 1, %d) AS h
			FROM jobs WHERE %s))) `,
		strings.Join(pick, ", "), strings.Join(digits, ", "), strings.Join(window, ", "), lastErrorHeadBytes, where)
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

// Window is the span job_failures actually covers. From and To are
// JST dates, To exclusive. Truncated is true when the requested period was
// longer than MaxDays and was cut to its last MaxDays days (the counts of the
// older days are not read, not zero).
type Window struct {
	From      string `json:"from"`
	To        string `json:"to"`
	MaxDays   int    `json:"max_days"`
	Truncated bool   `json:"truncated"`
	Note      string `json:"note"`
}

// Result is the failed jobs finished in Window by JST day, queue and
// error code (each list sums to the same total), and the jobs that ended as a
// terminal skip (status succeeded with a "skipped:" note) by queue. The error
// code is a token parsed out of jobs.last_error; the text itself is never
// returned. Only the six Queues are counted.
type Result struct {
	Window       Window         `json:"window"`
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

// window cuts [from, to) to its last MaxDays JST days.
func window(from, to time.Time) (time.Time, Window) {
	w := Window{MaxDays: MaxDays}
	if limit := to.AddDate(0, 0, -MaxDays); from.Before(limit) {
		from, w.Truncated = limit, true
	}
	w.From, w.To = from.In(jst).Format(time.DateOnly), to.In(jst).Format(time.DateOnly)
	w.Note = fmt.Sprintf("failed jobs and terminal skips are read for at most the last %d days of the period (default and ceiling) to keep collection_status fast; "+
		"jobs[] counts and the other fields cover the whole period", MaxDays)
	if w.Truncated {
		w.Note += fmt.Sprintf("; the requested period was longer, so only %s to %s (exclusive) is broken down here", w.From, w.To)
	}
	return from, w
}

// Read reads the failed and skipped jobs finished in the last
// MaxDays days of [periodFrom, periodTo). Each queue is read by its own
// statements so the (queue, status, finished_at) index answers the range
// (jobs_queue_status_finished_idx); the error head is reduced to its code
// token in SQL, so groups are (day, code), not one per message.
func Read(ctx context.Context, q Q, queues []string, periodFrom, periodTo time.Time) (Result, error) {
	from, win := window(periodFrom, periodTo)
	fromS, toS := sqlutil.FormatTime(from), sqlutil.FormatTime(periodTo)
	out := Result{Window: win}

	byDay, byQueue, byCode, byDayCode, skipped := map[string]int{}, map[string]int{}, map[string]int{}, map[[2]string]int{}, map[string]int{}
	failedSQL := errorCodeSQL("queue = ? AND status = 'failed' AND finished_at >= ? AND finished_at < ?")
	for _, queue := range queues {
		if err := failedByDayAndCode(ctx, q, failedSQL, queue, fromS, toS, func(day, code string, n int) {
			out.Total += n
			byDay[day] += n
			byQueue[queue] += n
			byCode[code] += n
			byDayCode[[2]string{day, code}] += n
		}); err != nil {
			return Result{}, err
		}
		var n int
		err := q.QueryRowContext(ctx, fmt.Sprintf(`
SELECT COUNT(*) FROM jobs
WHERE queue = ? AND status = 'succeeded' AND finished_at >= ? AND finished_at < ?
	AND substr(last_error, 1, %d) = '%s'`, len(skippedNotePrefix), skippedNotePrefix), queue, fromS, toS).Scan(&n)
		if err != nil {
			return Result{}, fmt.Errorf("query: job skips: %w", err)
		}
		skipped[queue] += n
		out.SkippedTotal += n
	}

	out.ByDay, out.ByQueue, out.ByErrorCode, out.ByDayAndCode, out.SkippedByQueue =
		[]DayCount{}, []QueueCount{}, []CodeCount{}, []DayCodeCount{}, []QueueCount{}
	for d, n := range byDay {
		out.ByDay = append(out.ByDay, DayCount{Date: d, Count: n})
	}
	sort.Slice(out.ByDay, func(i, j int) bool { return out.ByDay[i].Date < out.ByDay[j].Date })
	out.ByQueue = queueCounts(queues, byQueue)
	out.SkippedByQueue = queueCounts(queues, skipped)
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

// failedByDayAndCode runs an errorCodeSQL statement grouped by (day, code)
// with the arguments (queue, from, to) and calls add for each group, with the
// code normalised ("unknown" when the token is empty or malformed).
func failedByDayAndCode(ctx context.Context, q Q, stmt string, queue, from, to string, add func(day, code string, n int)) error {
	rows, err := q.QueryContext(ctx, `SELECT day, code, COUNT(*) FROM (`+stmt+`) GROUP BY 1, 2`, queue, from, to)
	if err != nil {
		return fmt.Errorf("query: job failures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day, code string
		var n int
		if err := rows.Scan(&day, &code, &n); err != nil {
			return fmt.Errorf("query: scan job failures: %w", err)
		}
		if !jobErrorCodeToken.MatchString(code) {
			code = jobErrorCodeNone
		}
		add(day, code, n)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query: job failures: %w", err)
	}
	return nil
}

// queueCounts lists counts in the order of queues; a queue name outside it
// (never written by the scheduler) is not reported by name but as "other".
func queueCounts(queues []string, counts map[string]int) []QueueCount {
	out := []QueueCount{}
	known := map[string]bool{}
	for _, name := range queues {
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

// jst is the zone of the reported days (the application's trading calendar).
var jst = time.FixedZone("JST", 9*60*60)

// Q is satisfied by *sql.Tx (and *sql.DB), as query.Q is.
type Q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
