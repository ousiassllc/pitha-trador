package indextests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/indexdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

func rangeTo(from, to string) query.Range {
	f, err := time.ParseInLocation(time.DateOnly, from, jst)
	if err != nil {
		panic(err)
	}
	e, err := time.ParseInLocation(time.DateOnly, to, jst)
	if err != nil {
		panic(err)
	}
	return query.Range{From: f, To: e}
}

// AtOrBefore returns, for each time, the latest value that was already taken (a
// minute's value is taken 5 seconds after its start) and whose minute is within the
// staleness bound; otherwise nil (unmatched). A value from after the time is never used.
func TestAtOrBeforeMatchesTheLatestKnownValueWithinTheStaleness(t *testing.T) {
	_, w := newServer(t)
	for _, c := range []struct {
		hm    string
		price float64
	}{{"09:00", 100}, {"09:01", 101}, {"09:10", 110}, {"13:00", 130}} {
		value(t, w, "101", "2026-10-15", c.hm, c.price)
	}
	value(t, w, "151", "2026-10-15", "09:01", 2700) // another code never matches

	d := func(hm string, sec int) time.Time { return at("2026-10-15", hm).Add(time.Duration(sec) * time.Second) }
	times := []time.Time{
		d("09:00", 4),  // 09:00's value is taken at 09:00:05: not yet known
		d("09:00", 5),  // known exactly then
		d("09:01", 30), // the 09:01 value
		d("09:06", 5),  // 09:01 is 5 minutes old: still matched
		d("09:06", 6),  // 09:01 is older than 5 minutes: unmatched
		d("08:59", 0),  // before the first value
		d("09:10", 5),
		d("11:40", 0), // the lunch break: the 09:10 value is far too old
		d("13:00", 0), // the 13:00 value is not taken yet
		d("09:01", 30),
	}
	got, err := indexdiag.AtOrBefore(t.Context(), w, "101", times, indexdiag.DefaultStaleness)
	if err != nil {
		t.Fatal(err)
	}
	wantPrice := []float64{0, 100, 101, 101, 0, 0, 110, 0, 0, 101}
	for i, p := range wantPrice {
		switch {
		case p == 0 && got[i] != nil:
			t.Errorf("time %d (%v) matched %+v, want unmatched", i, times[i], got[i])
		case p != 0 && (got[i] == nil || got[i].Price != p):
			t.Errorf("time %d (%v) = %+v, want price %v", i, times[i], got[i], p)
		}
	}
	got, err = indexdiag.AtOrBefore(t.Context(), w, "101", []time.Time{d("09:20", 0)}, 30*time.Minute)
	if err != nil || got[0] == nil || got[0].Price != 110 {
		t.Errorf("30-minute staleness = %+v, %v; want the 09:10 value", got[0], err)
	}
	if got, err = indexdiag.AtOrBefore(t.Context(), w, "101", nil, 0); err != nil || len(got) != 0 {
		t.Errorf("no times = %v, %v", got, err)
	}
	if got, err = indexdiag.AtOrBefore(t.Context(), w, "999", []time.Time{d("09:01", 30)}, 0); err != nil || got[0] != nil {
		t.Errorf("unknown code = %v, %v", got, err)
	}
}
