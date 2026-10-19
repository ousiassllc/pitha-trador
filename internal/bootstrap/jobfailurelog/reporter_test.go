package jobfailurelog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/jobfailurelog"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

var t0 = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

// record is one decoded log record.
type record map[string]any

func (r record) str(k string) string { s, _ := r[k].(string); return s }
func (r record) num(k string) int    { f, _ := r[k].(float64); return int(f) }

// captureLogs routes slog's default logger into the returned buffer.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func records(t *testing.T, buf *bytes.Buffer) []record {
	t.Helper()
	var out []record
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// fakeSource serves failures from memory with the real source's cursor
// semantics: strictly after the cursor in (finished_at, id) order, up to upTo.
type fakeSource struct {
	jobs  []jobqueue.FailedJob // sorted by (FinishedAt, ID)
	err   error
	calls int
}

func (f *fakeSource) ListFailedAfter(_ context.Context, c jobqueue.FailedCursor, upTo time.Time, limit int) ([]jobqueue.FailedJob, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []jobqueue.FailedJob
	for _, j := range f.jobs {
		after := j.FinishedAt.After(c.FinishedAt) || (j.FinishedAt.Equal(c.FinishedAt) && j.ID > c.ID)
		if after && !j.FinishedAt.After(upTo) && len(out) < limit {
			out = append(out, j)
		}
	}
	return out, nil
}

func failure(id int64, queue string, at time.Time, msg string) jobqueue.FailedJob {
	return jobqueue.FailedJob{ID: id, Queue: queue, FinishedAt: at, LastError: msg}
}

const kabuErr = "marketdata: kabu station api error (http 400, code 4001007): login"

func TestReport_AggregatesByCodeAndReportsEachFailureOnce(t *testing.T) {
	buf := captureLogs(t)
	src := &fakeSource{jobs: []jobqueue.FailedJob{
		failure(1, jobqueue.JobQueueMarketData, t0.Add(1*time.Second), kabuErr),
		failure(2, jobqueue.JobQueueMarketData, t0.Add(2*time.Second), kabuErr),
		failure(3, jobqueue.JobQueueJevScout, t0.Add(3*time.Second), "jev: api error (status 500): upstream"),
		failure(4, jobqueue.JobQueueMarketData, t0.Add(4*time.Second), kabuErr),
	}}
	rep := jobfailurelog.New(src, t0)
	ctx := context.Background()
	tick1 := t0.Add(time.Minute)

	if err := rep.Report(ctx, tick1); err != nil {
		t.Fatalf("Report: %v", err)
	}
	got := records(t, buf)
	if len(got) != 2 {
		t.Fatalf("records = %v, want 2 (one per error code)", got)
	}
	// Most frequent code first.
	if got[0].str("error_code") != "kabu_4001007" || got[0].num("count") != 3 || got[0].str("queues") != "market-data=3" {
		t.Errorf("first record = %v, want kabu_4001007 x3 on market-data", got[0])
	}
	if got[1].str("error_code") != "jev_http_500" || got[1].num("count") != 1 {
		t.Errorf("second record = %v, want jev_http_500 x1", got[1])
	}
	for _, r := range got {
		if r.str("level") != "ERROR" {
			t.Errorf("level = %q, want ERROR so that -error.log receives it", r.str("level"))
		}
		if r.str("message") == "" {
			t.Errorf("record %v has no representative message", r)
		}
	}
	if got[0].num("message_job_id") != 1 {
		t.Errorf("representative job = %d, want the oldest of the group (1)", got[0].num("message_job_id"))
	}

	// The same window again, then a window with nothing new: nothing logged.
	buf.Reset()
	if err := rep.Report(ctx, tick1); err != nil {
		t.Fatalf("Report again: %v", err)
	}
	if err := rep.Report(ctx, tick1.Add(time.Minute)); err != nil {
		t.Fatalf("Report quiet minute: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("re-logged already reported failures: %s", buf.String())
	}

	// A new failure is the only one reported on the next tick.
	src.jobs = append(src.jobs, failure(5, jobqueue.JobQueueMarketData, tick1.Add(80*time.Second), "orphaned: still running after 10m0s"))
	if err := rep.Report(ctx, tick1.Add(2*time.Minute)); err != nil {
		t.Fatalf("Report new failure: %v", err)
	}
	got = records(t, buf)
	if len(got) != 1 || got[0].str("error_code") != "orphaned" || got[0].num("count") != 1 {
		t.Fatalf("records = %v, want one orphaned x1", got)
	}
}

func TestReport_StartsAfterExistingHistory(t *testing.T) {
	buf := captureLogs(t)
	src := &fakeSource{jobs: []jobqueue.FailedJob{
		failure(1, jobqueue.JobQueueMarketData, t0.Add(-time.Hour), kabuErr), // an earlier run's failure
	}}
	rep := jobfailurelog.New(src, t0)
	if err := rep.Report(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("replayed history: %s", buf.String())
	}
}

func TestReport_HoldsBackTheLatestSecondsForUncommittedWrites(t *testing.T) {
	buf := captureLogs(t)
	tick := t0.Add(time.Minute)
	src := &fakeSource{jobs: []jobqueue.FailedJob{
		failure(1, jobqueue.JobQueueMarketData, tick.Add(-time.Second), kabuErr), // inside the grace
	}}
	rep := jobfailurelog.New(src, t0)
	if err := rep.Report(context.Background(), tick); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("reported a failure inside the grace period: %s", buf.String())
	}
	if err := rep.Report(context.Background(), tick.Add(time.Minute)); err != nil {
		t.Fatalf("Report next tick: %v", err)
	}
	if got := records(t, buf); len(got) != 1 || got[0].num("count") != 1 {
		t.Fatalf("records = %v, want the held-back failure reported on the next tick", got)
	}
}

func TestReport_ReadErrorKeepsTheCursor(t *testing.T) {
	buf := captureLogs(t)
	src := &fakeSource{
		jobs: []jobqueue.FailedJob{failure(1, jobqueue.JobQueueMarketData, t0.Add(time.Second), kabuErr)},
		err:  errors.New("database is locked"),
	}
	rep := jobfailurelog.New(src, t0)
	if err := rep.Report(context.Background(), t0.Add(time.Minute)); err == nil {
		t.Fatal("Report error = nil, want the read error")
	}
	src.err = nil
	if err := rep.Report(context.Background(), t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got := records(t, buf); len(got) != 1 || got[0].num("count") != 1 {
		t.Fatalf("records = %v, want the failure reported after the retry", got)
	}
}

func TestReport_BoundsRecordsPerTickAndKeepsReading(t *testing.T) {
	buf := captureLogs(t)
	var jobs []jobqueue.FailedJob
	// 25 distinct codes (kabu_<n>), the first one failing most often.
	id := int64(0)
	for code := 1; code <= 25; code++ {
		n := 1
		if code == 1 {
			n = 3
		}
		for range n {
			id++
			jobs = append(jobs, failure(id, jobqueue.JobQueueMarketData, t0.Add(time.Duration(id)*time.Millisecond),
				"marketdata: kabu station api error (http 400, code "+itoa(code)+")"))
		}
	}
	rep := jobfailurelog.New(&fakeSource{jobs: jobs}, t0)
	if err := rep.Report(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatalf("Report: %v", err)
	}
	got := records(t, buf)
	if len(got) != 21 {
		t.Fatalf("records = %d, want 20 per-code records plus one overflow record", len(got))
	}
	if got[0].str("error_code") != "kabu_1" || got[0].num("count") != 3 {
		t.Errorf("first record = %v, want the most frequent code first", got[0])
	}
	over := got[20]
	if over.num("omitted_error_codes") != 5 || over.num("omitted_count") != 5 {
		t.Errorf("overflow record = %v, want 5 omitted codes / 5 jobs", over)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestReport_MessageIsMaskedAndBounded(t *testing.T) {
	buf := captureLogs(t)
	secret := "marketdata: kabu station api error (http 401, code 4001007): " +
		`Bearer SECRETBEARERVALUE {"Symbol":"7203","CurrentPrice":3050.5} ` + strings.Repeat("y ", 500)
	src := &fakeSource{jobs: []jobqueue.FailedJob{failure(1, jobqueue.JobQueueMarketData, t0.Add(time.Second), secret)}}
	rep := jobfailurelog.New(src, t0)
	if err := rep.Report(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatalf("Report: %v", err)
	}
	out := buf.String()
	for _, leaked := range []string{"SECRETBEARERVALUE", "CurrentPrice", "3050", "7203"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log leaked %q: %s", leaked, out)
		}
	}
	got := records(t, buf)
	if len(got) != 1 || len(got[0].str("message")) > jobfailurelog.MaxMessageBytes {
		t.Fatalf("records = %v, want one with message <= %d bytes", got, jobfailurelog.MaxMessageBytes)
	}
	if got[0].str("error_code") != "kabu_4001007" {
		t.Errorf("error_code = %q, want kabu_4001007: the code survives the masking of the message", got[0].str("error_code"))
	}
}

func TestReport_ReadsBacklogInPagesAcrossTicks(t *testing.T) {
	buf := captureLogs(t)
	// More than maxPagesPerTick*pageSize (100*500) rows would be needed to
	// truncate; instead check that rows beyond one page are all counted.
	var jobs []jobqueue.FailedJob
	for i := int64(1); i <= 1200; i++ {
		jobs = append(jobs, failure(i, jobqueue.JobQueueMarketData, t0.Add(time.Duration(i)*time.Millisecond), kabuErr))
	}
	src := &fakeSource{jobs: jobs}
	rep := jobfailurelog.New(src, t0)
	if err := rep.Report(context.Background(), t0.Add(time.Minute)); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got := records(t, buf); len(got) != 1 || got[0].num("count") != 1200 {
		t.Fatalf("records = %v, want a single record counting all 1200 failures", got)
	}
	if src.calls != 3 {
		t.Errorf("ListFailedAfter calls = %d, want 3 pages (500+500+200)", src.calls)
	}
}
