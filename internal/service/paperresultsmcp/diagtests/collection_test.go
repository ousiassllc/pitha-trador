package diagtests

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// collection_status groups failures by broker error code, not by message: 700
// messages naming different symbols are two groups, and a burst older than the
// failure window (issue #850) is neither read nor counted in the breakdown,
// while the queue counts (jobs[]) still cover the whole period.
func TestJobFailuresGroupByCodeAndAreWindowed(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	tx, err := w.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ins := func(finished time.Time, lastError string) {
		fin := testkit.StoredAt(t, finished)
		if _, err := tx.ExecContext(t.Context(), `INSERT INTO jobs (queue, payload_json, status, scheduled_at, finished_at, last_error)
			VALUES ('market-data', '{}', 'failed', ?, ?, ?)`, fin, fin, lastError); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 600 {
		ins(at(20, 9, 0).Add(time.Duration(i)*time.Second),
			fmt.Sprintf(`marketdatajob: fetch board for "S%d": marketdata: kabu station api error (http 429, code 4001006): symbol name %d`, i, i))
	}
	for i := range 100 {
		ins(at(21, 9, 0).Add(time.Duration(i)*time.Second),
			fmt.Sprintf(`marketdatajob: fetch board for "S%d": marketdata: kabu station api error (http 401, code 4001007): name %d`, i, i))
	}
	ins(at(21, 10, 0), "tachibana: p_errno=-62: something")
	ins(at(21, 10, 1), "tachibana: sResultCode=11001 text")
	ins(at(21, 10, 2), "context deadline exceeded")
	ins(at(21, 10, 3), "code 12345678901 too long to be a code") // not a token: unknown
	for i := range 300 {                                         // a burst 15 days back, outside the 7-day window
		ins(at(6, 9, 0).Add(time.Duration(i)*time.Second), `kabu station api error (http 429, code 4001006): x`)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for _, p := range []query.Range{
		{From: at(22, 0, 0).AddDate(0, 0, -30), To: at(22, 0, 0)},
		period("2026-09-22", "2026-10-22"),
		period("2025-10-22", "2026-10-22"),
	} {
		c, err := query.CollectionStatus(t.Context(), w, p, diagNow)
		if err != nil {
			t.Fatal(err)
		}
		f := c.JobFailures
		if f.Total != 704 || f.Window.From != "2026-10-15" || f.Window.To != "2026-10-22" || f.Window.MaxDays != 7 || !f.Window.Truncated || f.Window.Note == "" {
			t.Errorf("period %v: failures = %+v, want the 704 failures of the last 7 days", p, f)
		}
		var codes []string
		for _, c := range f.ByErrorCode {
			codes = append(codes, fmt.Sprintf("%s=%d", c.Code, c.Count))
		}
		if want := []string{"4001006=600", "4001007=100", "unknown=2", "-62=1", "11001=1"}; !slices.Equal(codes, want) {
			t.Errorf("period %v: by_error_code = %v, want %v: one group per code, not per message", p, codes, want)
		}
		for _, j := range c.Jobs {
			if j.Queue == "market-data" && j.FailedCount != 1004 {
				t.Errorf("period %v: market-data failed_count = %d, want 1004 (the whole period)", p, j.FailedCount)
			}
			if j.Retired != (j.Queue == "feature-calc") || (j.Retired && !strings.Contains(j.RetiredNote, "retired")) || (!j.Retired && j.RetiredNote != "") {
				t.Errorf("job %s retired = %v %q, want feature-calc alone retired with a note", j.Queue, j.Retired, j.RetiredNote)
			}
		}
	}

	// A period inside the window is not truncated.
	c, err := query.CollectionStatus(t.Context(), w, period("2026-10-20", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.JobFailures.Window.Truncated || c.JobFailures.Window.From != "2026-10-20" || c.JobFailures.Total != 704 {
		t.Errorf("short period: failures = %+v, want 704 failures, not truncated", c.JobFailures)
	}
}
