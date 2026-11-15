package diagtests

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/sessiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// Issue #920: with kabu the days[].watch_list shows the size the runtime watch
// list had that day (watch_list_runtime_days), not "not applicable". A day with
// no record keeps watch_list_not_applicable; a day whose list was empty for the
// whole session is watch_list_empty (a real finding now); a list that filled and
// was later emptied is no finding. Only counts and the source are returned.
func TestKabuShowsTheRuntimeWatchListSize(t *testing.T) {
	w := diagDB(t)
	for _, r := range []struct {
		date                   string
		entries, peak, samples int
	}{
		{"2026-10-14", 40, 45, 180}, // a day that has no 立花 list either
		{"2026-10-20", 0, 0, 150},   // empty all session
		{"2026-10-21", 0, 12, 30},   // filled, then emptied
		{"2026-10-15", 33, 33, 170}, // 立花's saved list also exists (5 entries); kabu's runtime record wins
	} {
		testkit.MustExec(t, w, `INSERT INTO watch_list_runtime_days (session_date, source, entries, peak_entries, samples, recorded_at)
			VALUES (?, 'ranking', ?, ?, ?, ?)`, r.date, r.entries, r.peak, r.samples, testkit.StoredAt(t, diagNow))
	}
	d, err := sessiondiag.Read(t.Context(), w, period("2026-10-13", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	if d.TimedOut || d.BrokerProvider != "kabu" {
		t.Fatalf("timed_out = %v, broker_provider = %q", d.TimedOut, d.BrokerProvider)
	}
	obs := map[string][]string{}
	list := map[string]*sessiondiag.WatchListDay{}
	for _, day := range d.Days {
		obs[day.Date], list[day.Date] = day.Observations, day.WatchList
	}
	has := func(date, code string) bool {
		for _, o := range obs[date] {
			if o == code {
				return true
			}
		}
		return false
	}

	if l := list["2026-10-14"]; l == nil || !l.Present || l.Entries != 40 || l.PeakEntries == nil || *l.PeakEntries != 45 || l.Source == nil || *l.Source != "ranking" {
		t.Errorf("10-14 watch_list = %+v, want ranking, 40 entries, peak 45", l)
	}
	if l := list["2026-10-15"]; l == nil || l.Entries != 33 {
		t.Errorf("10-15 watch_list = %+v, want the runtime size 33 over the stored 5", l)
	}
	for _, date := range []string{"2026-10-14", "2026-10-15", "2026-10-21"} {
		for _, o := range obs[date] {
			if strings.HasPrefix(o, "watch_list_") {
				t.Errorf("%s: unexpected observation %s", date, o)
			}
		}
	}
	if !has("2026-10-20", sessiondiag.ObsWatchListEmpty) || has("2026-10-20", sessiondiag.ObsWatchListNotApplicable) {
		t.Errorf("10-20 (empty all session) observations = %v, want watch_list_empty", obs["2026-10-20"])
	}
	// 10-16 has an empty 立花 list and no runtime record: still not applicable.
	if !has("2026-10-16", sessiondiag.ObsWatchListNotApplicable) {
		t.Errorf("10-16 (no runtime record) observations = %v, want watch_list_not_applicable", obs["2026-10-16"])
	}
}

// The runtime record is kabu's: with 立花 selected the saved list is read as
// before and a stray runtime row (left from an earlier kabu period) is ignored.
func TestTachibanaIgnoresTheRuntimeWatchListSize(t *testing.T) {
	w := diagDB(t)
	testkit.Setting(t, w, config.KeyBrokerProvider, `"tachibana"`)
	testkit.MustExec(t, w, `INSERT INTO watch_list_runtime_days (session_date, source, entries, peak_entries, samples, recorded_at)
		VALUES ('2026-10-15', 'ranking', 33, 33, 170, ?), ('2026-10-20', 'ranking', 20, 20, 170, ?)`,
		testkit.StoredAt(t, diagNow), testkit.StoredAt(t, diagNow))
	d, err := sessiondiag.Read(t.Context(), w, period("2026-10-13", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range d.Days {
		switch day.Date {
		case "2026-10-15":
			if day.WatchList == nil || day.WatchList.Entries != 5 || day.WatchList.PeakEntries != nil {
				t.Errorf("10-15 watch_list = %+v, want the stored 立花 list of 5", day.WatchList)
			}
		case "2026-10-20":
			if day.WatchList == nil || day.WatchList.Present || !strings.Contains(strings.Join(day.Observations, ","), sessiondiag.ObsWatchListEmpty) {
				t.Errorf("10-20 = %+v %v, want no list and watch_list_empty as before", day.WatchList, day.Observations)
			}
		}
	}
}
