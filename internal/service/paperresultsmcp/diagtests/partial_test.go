package diagtests

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/sessiondiag"
)

// stallingQ holds the statements for which stall returns true until their ctx
// ends, as a statement that does not finish within the budget does; the others
// (and all after the ctx ended, which fail on their own) run against db.
type stallingQ struct {
	db    *sql.DB
	stall func(query string, args []any) bool
}

func (s stallingQ) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if s.stall(query, args) {
		<-ctx.Done()
	}
	return s.db.QueryContext(ctx, query, args...)
}

func (s stallingQ) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if s.stall(query, args) {
		<-ctx.Done()
	}
	return s.db.QueryRowContext(ctx, query, args...)
}

// When the budget runs out partway through the days, the days read newest
// first keep their counts, the rest are null and named in incomplete, and the
// response says timed_out instead of failing as a whole.
func TestPartialResultWhenTheBudgetRunsOut(t *testing.T) {
	w := diagDB(t)
	snapshotRows := 0
	q := stallingQ{db: w, stall: func(query string, _ []any) bool {
		if !strings.Contains(query, "FROM market_snapshots WHERE timestamp >= ?") {
			return false
		}
		snapshotRows++
		return snapshotRows == 3 // the third day read (10-19) does not finish in time
	}}
	// The call's own deadline is longer than the soft budget Read works to.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	d, err := sessiondiag.Read(ctx, q, period("2026-10-13", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatalf("Read = %v, want the partial result, not an error", err)
	}
	if !d.TimedOut {
		t.Fatal("timed_out = false, want true")
	}
	want := []string{
		"snapshot_rows:2026-10-19", "succeeded_without_snapshot:2026-10-19",
		"snapshot_rows:2026-10-16", "succeeded_without_snapshot:2026-10-16",
		"snapshot_rows:2026-10-15", "succeeded_without_snapshot:2026-10-15",
		"snapshot_rows:2026-10-14", "snapshot_rows:2026-10-13",
	}
	if !slices.Equal(d.Incomplete, want) {
		t.Errorf("incomplete = %v, want %v", d.Incomplete, want)
	}
	if d.Current == nil || len(d.Days) != 7 {
		t.Fatalf("current = %v, days = %d: what was read before the budget ran out stays", d.Current, len(d.Days))
	}
	byDate := map[string]sessiondiag.Day{}
	for _, day := range d.Days {
		byDate[day.Date] = day
	}
	for _, date := range []string{"2026-10-20", "2026-10-21"} { // read in time
		day := byDate[date]
		if day.SnapshotRows == nil || day.MarketDataJob.SucceededWithoutSnapshot == nil {
			t.Errorf("%s = %+v, want its counts", date, day)
		}
	}
	for _, date := range []string{"2026-10-19", "2026-10-16", "2026-10-15", "2026-10-14", "2026-10-13"} { // not read
		day := byDate[date]
		if day.SnapshotRows != nil || day.MarketDataJob.SucceededWithoutSnapshot != nil || slices.Contains(day.Observations, sessiondiag.ObsNoSnapshots) {
			t.Errorf("%s = %+v, want null counts and no observation drawn from them", date, day)
		}
	}
	// The statements before the days were in time: job counts and watch lists stay.
	if f := byDate["2026-10-15"].MarketDataJob.Failed; f == nil || *f != 3 || byDate["2026-10-15"].WatchList == nil || byDate["2026-10-15"].WatchList.Entries != 5 {
		t.Errorf("10-15 = %+v, want its job counts and watch list from the statements read in time", byDate["2026-10-15"])
	}

	// JSON: the mark and the list are present, and nothing but counts and fixed codes.
	b, err := json.Marshal(d)
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
	if strings.Contains(string(b), "payload") || strings.Contains(string(b), "7203") || strings.Contains(string(b), "SENTINEL") {
		t.Errorf("json leaks job or symbol text: %s", b)
	}
}

// Everything in time: no mark, an empty (not null) list.
func TestCompleteResultIsNotMarkedTimedOut(t *testing.T) {
	d, err := sessiondiag.Read(t.Context(), diagDB(t), period("2026-10-13", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(d)
	if d.TimedOut || d.Incomplete == nil || len(d.Incomplete) != 0 || !strings.Contains(string(b), `"timed_out":false`) || !strings.Contains(string(b), `"incomplete":[]`) {
		t.Errorf("timed_out = %v incomplete = %v, want false and an empty list", d.TimedOut, d.Incomplete)
	}
}

// A call whose own deadline has passed is the server's query_timeout, not a partial result.
func TestEndedContextIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sessiondiag.Read(ctx, diagDB(t), period("2026-10-13", "2026-10-22"), diagNow); err == nil {
		t.Fatal("Read of an ended context returned no error")
	}
}
