package jobfailures

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// errorCodeSQL reduces each message to its code token inside SQL: 700
// messages that differ only in the symbol name are two groups, and the
// realistic formats give the codes the former Go regexp
// (`(?:\bcode |p_errno=|sResultCode=)(-?[0-9]{1,8})\b`) gave.
func TestErrorCodeSQLReducesMessagesToCodes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE jobs (queue TEXT, status TEXT, finished_at TEXT, last_error TEXT)`); err != nil {
		t.Fatal(err)
	}
	const finished = "2026-10-20T01:00:00Z"
	insert := func(msg any) {
		if _, err := db.Exec(`INSERT INTO jobs VALUES ('market-data', 'failed', ?, ?)`, finished, msg); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 600 {
		insert(fmt.Sprintf(`marketdatajob: fetch board for "S%d": marketdata: kabu station api error (http 429, code 4001006): name %d`, i, i))
	}
	for i := range 100 {
		insert(fmt.Sprintf(`marketdatajob: fetch board for "S%d": marketdata: kabu station api error (http 401, code 4001007): name %d`, i, i))
	}
	single := map[string]string{
		"tachibana: p_errno=-62: text":                      "-62",
		"tachibana: sResultCode=11001 text":                 "11001",
		"context deadline exceeded":                         "",
		"code 12345678901 too long":                         "12345678901", // SQL keeps 10 digits; the Go side rejects it
		"kabu station api error (http 400, code 4002006)":   "4002006",
		"kabu station api error (http 400, code 4002006)  ": "4002006",
	}
	for msg := range single {
		insert(msg)
	}
	insert(nil)

	rows, err := db.Query(`SELECT day, code, COUNT(*) FROM (`+errorCodeSQL("queue = ? AND status = 'failed' AND finished_at >= ? AND finished_at < ?")+`) GROUP BY 1, 2`,
		"market-data", "2026-10-19T00:00:00Z", "2026-10-21T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]int{}
	for rows.Next() {
		var day, code string
		var n int
		if err := rows.Scan(&day, &code, &n); err != nil {
			t.Fatal(err)
		}
		if day != "2026-10-20" {
			t.Errorf("day = %q, want the JST day 2026-10-20", day)
		}
		got[code] += n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"4001006": 600, "4001007": 100, "4002006": 2, "-62": 1, "11001": 1,
		"":           2, // no code token (the message without one, and NULL)
		"1234567890": 1, // 10 digits cut from "code 12345678901": not a token, so Go-side check maps it to unknown
	}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v: one per code, not per message", got, want)
	}
	for code, n := range want {
		if got[code] != n {
			t.Errorf("code %q = %d, want %d (all: %v)", code, got[code], n, got)
		}
	}
	if !jobErrorCodeToken.MatchString("4001006") || jobErrorCodeToken.MatchString("1234567890") {
		t.Error("jobErrorCodeToken must accept 7 digits and reject 10")
	}
}

// stallingQ runs the statements for which stall returns false against db and
// holds every other one until its ctx ends, as a statement that does not
// finish within the budget does.
type stallingQ struct {
	db    *sql.DB
	stall func(query string) bool
}

func (s stallingQ) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if s.stall(query) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.db.QueryContext(ctx, query, args...)
}

func (s stallingQ) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if s.stall(query) {
		<-ctx.Done()
	}
	return s.db.QueryRowContext(ctx, query, args...) // an ended ctx fails the Scan
}

func seededJobs(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE jobs (queue TEXT, status TEXT, finished_at TEXT, last_error TEXT)`); err != nil {
		t.Fatal(err)
	}
	// One failure per queue per JST day 10-19, 10-20, 10-21 (JST 09:00 = 00:00Z).
	for _, queue := range []string{"market-data", "jev-scout"} {
		for day := 19; day <= 21; day++ {
			if _, err := db.Exec(`INSERT INTO jobs VALUES (?, 'failed', ?, 'kabu station api error (http 429, code 4001006)')`,
				queue, fmt.Sprintf("2026-10-%dT00:00:00Z", day)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

var (
	jobsFrom = time.Date(2026, 10, 19, 0, 0, 0, 0, jst)
	jobsTo   = time.Date(2026, 10, 22, 0, 0, 0, 0, jst)
	twoQueue = []string{"market-data", "jev-scout"}
)

func TestReadCompleteReportsNoTimeout(t *testing.T) {
	r, err := Read(t.Context(), seededJobs(t), twoQueue, jobsFrom, jobsTo)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 6 || r.TimedOut || r.Incomplete == nil || len(r.Incomplete) != 0 {
		t.Fatalf("result = %+v, want 6 failures, not timed out, an empty (non-null) incomplete", r)
	}
}

// When the budget runs out the groups already read are returned, newest day first, with timed_out and exactly what was not read.
func TestReadReturnsWhatItHasWhenTheBudgetRunsOut(t *testing.T) {
	db := seededJobs(t)
	failedStatements := 0
	q := stallingQ{db: db, stall: func(query string) bool {
		if !isFailedStatement(query) {
			return false
		}
		failedStatements++
		return failedStatements == 5 // the fifth (market-data, 10-20) does not finish
	}}
	// The call's own deadline (the server's QueryTimeout) is longer than the
	// soft budget Read works to, so the call itself does not fail.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	r, err := Read(ctx, q, twoQueue, jobsFrom, jobsTo)
	if err != nil {
		t.Fatalf("Read = %v, want the partial result, not an error", err)
	}
	if !r.TimedOut || r.Total != 4 || len(r.ByDay) != 3 || r.ByDay[0] != (DayCount{"2026-10-19", 1}) || r.ByDay[1] != (DayCount{"2026-10-20", 1}) || r.ByDay[2] != (DayCount{"2026-10-21", 2}) {
		t.Errorf("result = %+v, want timed out with jev-scout's 3 failures and market-data's 10-21 one", r)
	}
	// market-data (the busiest queue) is read last, newest day first; the
	// statements after the one that ran out of time fail on their own. The skip
	// counts were read first, so none of them is missing.
	want := []string{"failed:market-data:2026-10-20", "failed:market-data:2026-10-19"}
	if !slices.Equal(r.Incomplete, want) {
		t.Errorf("incomplete = %v, want %v", r.Incomplete, want)
	}
	if len(r.ByErrorCode) != 1 || r.ByErrorCode[0].Code != "4001006" || r.ByErrorCode[0].Count != 4 {
		t.Errorf("by_error_code = %+v, want the 4 read failures as 4001006", r.ByErrorCode)
	}

	// The JSON form: the mark and the list, lists never null, no message text.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["timed_out"] != true || len(m["incomplete"].([]any)) != len(want) {
		t.Errorf("json = %s", b)
	}
	for _, k := range []string{"by_day", "by_queue", "by_error_code", "by_day_and_error_code", "skipped_by_queue", "incomplete"} {
		if _, ok := m[k].([]any); !ok {
			t.Errorf("json %q = %v, want a list, not null", k, m[k])
		}
	}
}

// A call whose own deadline has passed is the server's query_timeout: Read
// returns the error, not a partial result.
func TestReadFailsWhenTheCallsOwnContextEnded(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Read(ctx, seededJobs(t), twoQueue, jobsFrom, jobsTo); err == nil {
		t.Fatal("Read of an ended context returned no error")
	}
}

// isFailedStatement is a failed-jobs statement (errorCodeSQL), not the skip count.
func isFailedStatement(query string) bool { return strings.Contains(query, "status = 'failed'") }
