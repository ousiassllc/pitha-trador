// Package jobfailures is the failed-job breakdown of collection_status
// (issues #801, #850): by JST day, queue and broker error code, from a bounded
// window and without ever returning job text.
package jobfailures

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/budget"
)

// lastErrorHeadBytes bounds how much of jobs.last_error one statement reads
// (the worker already truncates the column to 1 KiB). The head is reduced to
// a code token inside SQL (errorCodeSQL) and dropped: only a fixed code
// token, a queue, a JST day and counts are ever returned.
const lastErrorHeadBytes = 256

// MaxDays is the longest span of job_failures: the failed-job breakdown
// covers at most this many JST days ending at the period's end, so a 30- or
// 366-day collection_status does not scan a failure burst of weeks ago (issue
// #850). It is also the default, as the default 30-day period is cut to the
// same span. 3 days is the owner's decision (a 220,838-row burst inside the
// window still takes about 1.5 s warm and 4 s cold); a burst inside even these days can still spend the time
// budget, in which case Read returns the part it has (Result.TimedOut).
const MaxDays = 3

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
	From          string `json:"from"`
	To            string `json:"to"`
	RequestedFrom string `json:"requested_from"`
	MaxDays       int    `json:"max_days"`
	Truncated     bool   `json:"truncated"`
	Note          string `json:"note"`
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
	// TimedOut is true when the soft time budget (package budget) ran out: every
	// count above then covers only what was read, and Incomplete lists what was
	// not ("failed:<queue>:<JST date>" and "skipped:<queue>"). It is false and
	// Incomplete empty when everything in Window was read.
	TimedOut   bool     `json:"timed_out"`
	Incomplete []string `json:"incomplete"`
}

// window cuts [from, to) to its last MaxDays JST days.
func window(from, to time.Time) (time.Time, Window) {
	w := Window{MaxDays: MaxDays, RequestedFrom: from.In(jst).Format(time.DateOnly)}
	if limit := to.AddDate(0, 0, -MaxDays); from.Before(limit) {
		from, w.Truncated = limit, true
	}
	w.From, w.To = from.In(jst).Format(time.DateOnly), to.In(jst).Format(time.DateOnly)
	w.Note = fmt.Sprintf("failed jobs and terminal skips are read for at most the last %d days of the period (default and ceiling) to keep collection_status fast; "+
		"jobs[] counts and the other fields cover the whole period; when timed_out is true only the part listed as not incomplete was read", MaxDays)
	if w.Truncated {
		w.Note += fmt.Sprintf("; the requested period was longer, so only %s to %s (exclusive) is broken down here", w.From, w.To)
	}
	return from, w
}

// Read reads the failed and skipped jobs finished in the last MaxDays days of
// [periodFrom, periodTo). Failures are read per queue (the busiest last) and
// JST day (newest first) so the (queue, status, finished_at) index answers each range
// (jobs_queue_status_finished_idx) and a spent time budget loses only the
// statements not yet run; the error head is reduced to its code token in SQL,
// so groups are (day, code), not one per message. When the soft budget
// (package budget) runs out, Read returns what it has with TimedOut set.
func Read(ctx context.Context, q Q, queues []string, periodFrom, periodTo time.Time) (Result, error) {
	from, win := window(periodFrom, periodTo)
	out := Result{Window: win, Incomplete: []string{}}
	soft, cancel := budget.Soft(ctx)
	defer cancel()

	byDay, byQueue, byCode, byDayCode, skipped := map[string]int{}, map[string]int{}, map[string]int{}, map[[2]string]int{}, map[string]int{}
	failedSQL := errorCodeSQL("queue = ? AND status = 'failed' AND finished_at >= ? AND finished_at < ?")
	var days [][2]time.Time
	for d := from; d.Before(periodTo); d = d.AddDate(0, 0, 1) {
		end := d.AddDate(0, 0, 1)
		if end.After(periodTo) {
			end = periodTo
		}
		days = append(days, [2]time.Time{d, end})
	}
	// The skip counts are cheap (index range, few rows), so they go first: a
	// failed-job statement that spends the budget must not take them with it.
	for _, queue := range queues {
		var n int
		err := q.QueryRowContext(soft, fmt.Sprintf(`
SELECT COUNT(*) FROM jobs
WHERE queue = ? AND status = 'succeeded' AND finished_at >= ? AND finished_at < ?
	AND substr(last_error, 1, %d) = '%s'`, len(skippedNotePrefix), skippedNotePrefix),
			queue, sqlutil.FormatTime(from), sqlutil.FormatTime(periodTo)).Scan(&n)
		if err != nil {
			if !budget.Spent(ctx, soft, err) {
				return Result{}, fmt.Errorf("query: job skips: %w", err)
			}
			out.TimedOut = true
			out.Incomplete = append(out.Incomplete, "skipped:"+queue)
			continue
		}
		skipped[queue] += n
		out.SkippedTotal += n
	}

	// The busiest queue goes last, so the statements that can spend the budget
	// come after every cheap one; within a queue, the newest day goes first.
	for _, queue := range busiestLast(queues) {
		for i := len(days) - 1; i >= 0; i-- {
			fromS, toS := sqlutil.FormatTime(days[i][0]), sqlutil.FormatTime(days[i][1])
			groups, err := failedByDayAndCode(soft, q, failedSQL, queue, fromS, toS)
			if err != nil {
				if !budget.Spent(ctx, soft, err) {
					return Result{}, err
				}
				out.TimedOut = true
				out.Incomplete = append(out.Incomplete, fmt.Sprintf("failed:%s:%s", queue, days[i][0].In(jst).Format(time.DateOnly)))
				continue
			}
			for _, g := range groups {
				out.Total += g.n
				byDay[g.day] += g.n
				byQueue[queue] += g.n
				byCode[g.code] += g.n
				byDayCode[[2]string{g.day, g.code}] += g.n
			}
		}
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

// busiestQueue is the queue that carries one job per symbol per cycle: orders
// of magnitude more rows than the others, so the one statement likely to
// spend the time budget.
const busiestQueue = "market-data"

// busiestLast is queues with busiestQueue, if present, moved to the end.
func busiestLast(queues []string) []string {
	out := make([]string, 0, len(queues))
	for _, q := range queues {
		if q != busiestQueue {
			out = append(out, q)
		}
	}
	if slices.Contains(queues, busiestQueue) {
		out = append(out, busiestQueue)
	}
	return out
}

// group is one (day, code) group of a failed-jobs statement.
type group struct {
	day, code string
	n         int
}

// failedByDayAndCode runs an errorCodeSQL statement grouped by (day, code)
// with the arguments (queue, from, to) and returns the groups, with the code
// normalised ("unknown" when the token is empty or malformed). It returns
// all of them or an error, never a part.
func failedByDayAndCode(ctx context.Context, q Q, stmt string, queue, from, to string) ([]group, error) {
	rows, err := q.QueryContext(ctx, `SELECT day, code, COUNT(*) FROM (`+stmt+`) GROUP BY 1, 2`, queue, from, to)
	if err != nil {
		return nil, fmt.Errorf("query: job failures: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.day, &g.code, &g.n); err != nil {
			return nil, fmt.Errorf("query: scan job failures: %w", err)
		}
		if !jobErrorCodeToken.MatchString(g.code) {
			g.code = jobErrorCodeNone
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: job failures: %w", err)
	}
	return groups, nil
}

// queueCounts lists the non-zero counts in the order of queues. Only those
// queues are read, so no other queue name can appear in counts.
func queueCounts(queues []string, counts map[string]int) []QueueCount {
	out := []QueueCount{}
	for _, name := range queues {
		if n := counts[name]; n > 0 {
			out = append(out, QueueCount{Queue: name, Count: n})
		}
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
