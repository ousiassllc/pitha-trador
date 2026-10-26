package diagtests

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/sessiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// failingProviderRead is a database whose read of broker.provider fails (the
// other statements are the real ones).
type failingProviderRead struct {
	query.Q
	db *sql.DB
}

func (f failingProviderRead) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if len(args) == 1 && args[0] == config.KeyBrokerProvider {
		return f.db.QueryRowContext(ctx, `SELECT value FROM a_table_that_does_not_exist`)
	}
	return f.Q.QueryRowContext(ctx, q, args...)
}

// Issue #914: only 立花 writes watch_lists, so with kabu (the default) an empty
// table is not a finding about the watch list. The same days that read
// watch_list_empty under 立花 read watch_list_not_applicable under kabu; a day
// that has a list reads neither; a broker selection that cannot be read leaves
// the old judgement and says so; no case fails the tool.
func TestWatchListObservationFollowsTheBroker(t *testing.T) {
	// 10-14, 10-20 and 10-21 have no list and 10-16 an empty one (diagDB); 10-13,
	// 10-15 and 10-19 have one.
	emptyDays := []string{"2026-10-14", "2026-10-16", "2026-10-20", "2026-10-21"}
	for _, tc := range []struct {
		name     string
		stored   string // runtime_settings broker.provider ("": no row)
		fail     bool
		provider string
		obs      string
	}{
		{"nothing stored is kabu", "", false, "kabu", sessiondiag.ObsWatchListNotApplicable},
		{"kabu", `"kabu"`, false, "kabu", sessiondiag.ObsWatchListNotApplicable},
		{"tachibana", `"tachibana"`, false, "tachibana", sessiondiag.ObsWatchListEmpty},
		// The app ignores a stored value it cannot parse and starts with the default.
		{"unknown name", `"bogus"`, false, "kabu", sessiondiag.ObsWatchListNotApplicable},
		{"not a string", `5`, false, "kabu", sessiondiag.ObsWatchListNotApplicable},
		{"not JSON", `{`, false, "kabu", sessiondiag.ObsWatchListNotApplicable},
		{"read fails", "", true, sessiondiag.ProviderUnknown, sessiondiag.ObsWatchListEmpty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := diagDB(t)
			if tc.stored != "" {
				testkit.Setting(t, w, config.KeyBrokerProvider, tc.stored)
			}
			var q query.Q = w
			if tc.fail {
				q = failingProviderRead{Q: w, db: w}
			}
			d, err := sessiondiag.Read(t.Context(), q, period("2026-10-13", "2026-10-22"), diagNow)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if d.BrokerProvider != tc.provider || d.TimedOut {
				t.Errorf("broker_provider = %q timed_out = %v, want %q", d.BrokerProvider, d.TimedOut, tc.provider)
			}
			var gotEmpty []string
			for _, day := range d.Days {
				for _, o := range day.Observations {
					if strings.HasPrefix(o, "watch_list_") {
						if o != tc.obs {
							t.Errorf("%s: observation %s, want %s", day.Date, o, tc.obs)
						}
						gotEmpty = append(gotEmpty, day.Date)
					}
				}
				if day.WatchList == nil {
					t.Errorf("%s: the watch list rows were not read", day.Date)
				}
			}
			if !slices.Equal(gotEmpty, emptyDays) {
				t.Errorf("days with a watch list observation = %v, want %v (the days with no or an empty list)", gotEmpty, emptyDays)
			}
		})
	}
}

// With kabu the table still reports what it holds (a list written while 立花 was
// selected is real data): the observation only replaces watch_list_empty.
func TestKabuKeepsTheStoredWatchListRows(t *testing.T) {
	w := diagDB(t)
	d, err := sessiondiag.Read(t.Context(), w, period("2026-10-13", "2026-10-22"), diagNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range d.Days {
		if day.Date == "2026-10-15" && (day.WatchList == nil || !day.WatchList.Present || day.WatchList.Entries != 5) {
			t.Errorf("10-15 watch list = %+v, want the 5 stored entries", day.WatchList)
		}
	}
}
