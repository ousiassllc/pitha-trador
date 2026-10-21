package diagtests

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// The failures by JST day, queue and broker error code (the code is the only
// thing parsed out of last_error) and the skip counts, on the database the
// other collection_status tests seed (issues #801, #850). Only the last 3 days
// of a period are read, so each part is asserted on a period whose last 3 days
// hold it.
func TestSeededJobFailures(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	testkit.SeedCollection(t, w)
	read := func(from, to string) query.Collection {
		c, err := query.CollectionStatus(t.Context(), w, period(from, to), diagNow)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// The period 10-01..10-10 reads 10-07..10-09: only the -62 failure.
	if f := read("2026-10-01", "2026-10-10").JobFailures; f.Total != 1 || f.Window.From != "2026-10-07" || f.Window.To != "2026-10-10" ||
		f.Window.MaxDays != 3 || !f.Window.Truncated || f.TimedOut || len(f.Incomplete) != 0 {
		t.Errorf("job_failures of 10-01..10-10 = %+v, want the 1 failure of 10-07..10-09, complete", f)
	}

	// Window 10-05..10-07: the 10-06 and 10-07 failures, not the 10-03 one.
	f := read("2026-10-01", "2026-10-08").JobFailures
	if f.Total != 4 {
		t.Fatalf("job_failures.total = %d, want 4 (2 market-data + 2 jev-scout of 10-06..10-07; 10-03 and 09-01 are outside)", f.Total)
	}
	var byDay, byQueue, byCode, byDayCode []string
	for _, x := range f.ByDay {
		byDay = append(byDay, fmt.Sprintf("%s=%d", x.Date, x.Count))
	}
	for _, x := range f.ByQueue {
		byQueue = append(byQueue, fmt.Sprintf("%s=%d", x.Queue, x.Count))
	}
	for _, x := range f.ByErrorCode {
		byCode = append(byCode, fmt.Sprintf("%s=%d", x.Code, x.Count))
	}
	for _, x := range f.ByDayAndCode {
		byDayCode = append(byDayCode, fmt.Sprintf("%s/%s=%d", x.Date, x.Code, x.Count))
	}
	for name, c := range map[string]struct{ got, want []string }{
		"by_day":                {byDay, []string{"2026-10-06=3", "2026-10-07=1"}},
		"by_queue":              {byQueue, []string{"market-data=2", "jev-scout=2"}},
		"by_error_code":         {byCode, []string{"4002006=2", "-62=1", "4001007=1"}},
		"by_day_and_error_code": {byDayCode, []string{"2026-10-06/4002006=2", "2026-10-06/4001007=1", "2026-10-07/-62=1"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("job_failures.%s = %v, want %v", name, c.got, c.want)
		}
	}

	// Window 10-04..10-06 holds the two terminal skips of 10-04 (a deferred note is not one).
	if f := read("2026-10-01", "2026-10-07").JobFailures; f.SkippedTotal != 2 || len(f.SkippedByQueue) != 1 ||
		f.SkippedByQueue[0].Queue != "outcome-labeling" || f.SkippedByQueue[0].Count != 2 {
		t.Errorf("skipped = %d %+v, want 2 outcome-labeling terminal skips", f.SkippedTotal, f.SkippedByQueue)
	}
}
