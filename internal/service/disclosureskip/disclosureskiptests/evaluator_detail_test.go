package disclosureskiptests

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/disclosureskip"
)

func detailOf(t *testing.T, conn *sql.DB, decisionID int) string {
	t.Helper()
	var detail sql.NullString
	if err := conn.QueryRow(`SELECT unknown_detail FROM disclosure_skip_records WHERE jev_decision_id = ?`, decisionID).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	return detail.String
}

// A decision at the end of the morning session is not given up on at 12:25,
// in the middle of the lunch break, when nothing can be fetched: the wait is
// counted in session time, so the fetch the afternoon session brings still
// covers it. A disclosure published after the decision stays invisible.
func TestEvaluator_LunchBreakSpanningDecisionWaitsForTheAfternoonSession(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 11, 25, 0)
	now := jstAt(10, 14, 12, 29, 0) // the old 60-minute give-up passed at 12:25
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
	insertDecision(t, conn, 1, "trader", "LONG", decided)

	// A fetch at 11:29 is inside the lag: no coverage yet.
	if err := store.WriteFetch(context.Background(), disclosureskip.FetchEvent{Symbol: sym, At: jstAt(10, 14, 11, 29, 0), OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := recordOf(t, conn, 1); ok {
		t.Fatal("the decision was given up on in the lunch break")
	}

	// The afternoon session's first fetch lists an earnings report published at
	// 12:40, after the decision: it must not turn the decision into a skip.
	now = jstAt(10, 14, 12, 41, 0)
	fetchInto(t, store, jstAt(10, 14, 12, 40, 30), tdnet("late", tanshin, jstAt(10, 14, 12, 40, 0)))
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, ok := recordOf(t, conn, 1); !ok || r.verdict != "no_skip" || r.matches != 0 {
		t.Fatalf("record = %+v, %v; want no_skip with no match (the disclosure is after the decision)", r, ok)
	}
}

// The same for a decision just before the close: it waits for the next trading
// session instead of ending as unknown / no_fetch at 16:27, and only the
// disclosures up to the decision count.
func TestEvaluator_CloseSpanningDecisionWaitsForTheNextSession(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 15, 27, 0)
	now := jstAt(10, 15, 9, 10, 0)
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
	insertDecision(t, conn, 1, "trader", "SHORT", decided)
	insertDecision(t, conn, 2, "trader", "LONG", decided.Add(-time.Hour)) // decided 14:27, covered by nothing later today either

	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := recordOf(t, conn, 1); ok {
		t.Fatal("a decision at 15:27 was given up on before the next session")
	}

	// The next morning's fetch lists the report published at 15:40 (after the
	// decision) and one at 15:20 (before it).
	fetchInto(t, store, jstAt(10, 15, 9, 5, 0), tdnet("after", tanshin, jstAt(10, 14, 15, 40, 0)), tdnet("before", "業績予想の修正に関するお知らせ", jstAt(10, 14, 15, 20, 0)))
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, ok := recordOf(t, conn, 1)
	if !ok || r.verdict != "skip" || r.matches != 1 {
		t.Fatalf("record = %+v, %v; want a skip on the one disclosure before the decision", r, ok)
	}
	var kind string
	if err := conn.QueryRow(`SELECT kind FROM disclosure_skip_matches`).Scan(&kind); err != nil || kind != "forecast_revision" {
		t.Fatalf("match kind = %q, %v; the 15:40 earnings report is the future and must not match", kind, err)
	}
}

// When no covering fetch ever comes, the record says why, and the reason is
// stored with the record: it is still there after the per-fetch rows are gone.
func TestEvaluator_StoresTheUnknownDetailAndKeepsItWhenFetchesArePruned(t *testing.T) {
	for _, tc := range []struct {
		name       string
		decided    time.Time
		fetches    []disclosureskip.FetchEvent
		now        time.Time
		wantReason string
		wantDetail string
	}{
		{
			name: "dropped from the candidates", decided: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.FetchEvent{{Symbol: sym, At: jstAt(10, 14, 9, 59, 0), OK: true}},
			now:        jstAt(10, 14, 11, 1, 0),
			wantReason: "no_fetch", wantDetail: "dropped_from_candidates",
		},
		{
			name: "within the fetch lag", decided: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.FetchEvent{{Symbol: sym, At: jstAt(10, 14, 10, 2, 0), OK: true}},
			now:        jstAt(10, 14, 11, 1, 0),
			wantReason: "no_fetch", wantDetail: "within_fetch_lag",
		},
		{
			name: "fetch failed", decided: jstAt(10, 14, 10, 0, 0),
			fetches:    []disclosureskip.FetchEvent{{Symbol: sym, At: jstAt(10, 14, 10, 6, 0)}, {Symbol: sym, At: jstAt(10, 14, 10, 40, 0)}},
			now:        jstAt(10, 14, 11, 1, 0),
			wantReason: "no_fetch", wantDetail: "fetch_failed",
		},
		{
			name: "lunch break, and the afternoon never covered it", decided: jstAt(10, 14, 11, 27, 0),
			now:        jstAt(10, 14, 13, 28, 0),
			wantReason: "no_fetch", wantDetail: "lunch_break",
		},
		{
			name: "after the close, and the next morning never covered it", decided: jstAt(10, 14, 15, 27, 0),
			now:        jstAt(10, 15, 10, 28, 0),
			wantReason: "no_fetch", wantDetail: "after_close",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := openDB(t)
			store := disclosureskip.NewStore(conn)
			now := tc.now
			ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
			insertDecision(t, conn, 1, "trader", "LONG", tc.decided)
			for _, f := range tc.fetches {
				if err := store.WriteFetch(context.Background(), f); err != nil {
					t.Fatal(err)
				}
			}
			if err := ev.Cycle(context.Background()); err != nil {
				t.Fatal(err)
			}
			r, ok := recordOf(t, conn, 1)
			if !ok || r.verdict != "unknown" || r.reason != tc.wantReason {
				t.Fatalf("record = %+v, %v; want unknown / %s", r, ok, tc.wantReason)
			}
			if got := detailOf(t, conn, 1); got != tc.wantDetail {
				t.Fatalf("unknown_detail = %q, want %q", got, tc.wantDetail)
			}
			// 8 days later the fetch rows are pruned; the record keeps its reason.
			now = tc.now.Add(8 * 24 * time.Hour)
			if err := ev.Cycle(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := countRows(t, conn, "disclosure_fetches"); got != 0 {
				t.Fatalf("fetches = %d, want them pruned", got)
			}
			if got := detailOf(t, conn, 1); got != tc.wantDetail {
				t.Fatalf("after the prune: unknown_detail = %q, want %q", got, tc.wantDetail)
			}
		})
	}
}

// A verdict other than unknown carries no detail, and an incomplete fetch gets
// its own.
func TestEvaluator_OnlyUnknownRecordsHaveADetail(t *testing.T) {
	conn := openDB(t)
	store := disclosureskip.NewStore(conn)
	decided := jstAt(10, 14, 10, 0, 0)
	now := decided.Add(20 * time.Minute)
	ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return now }))
	insertDecision(t, conn, 1, "trader", "LONG", decided)
	insertDecision(t, conn, 2, "trader", "LONG", decided.Add(time.Minute))
	fetchInto(t, store, decided.Add(10*time.Minute), tdnet("a", tanshin, jstAt(10, 14, 9, 0, 0)))
	if err := ev.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		if r, ok := recordOf(t, conn, id); !ok || r.verdict != "skip" || detailOf(t, conn, id) != "" {
			t.Fatalf("decision %d: %+v, %v, detail %q", id, r, ok, detailOf(t, conn, id))
		}
	}
}

// An application that was down when a decision's give-up time passed still
// records the decision after the restart, for 5 hours past the give-up time and
// no longer (then it is "not recorded"). The give-up time is in session time, so
// a Friday-afternoon decision, whose give-up time is Monday morning, is not
// dropped over the weekend: the limit is never shorter than the old 6 hours.
func TestEvaluator_RecordsADecisionAfterARestartUpToFiveHoursPastItsGiveUpTime(t *testing.T) {
	cases := []struct {
		name    string
		decided time.Time
		restart time.Time
		want    bool
	}{
		{"same session, 4h59m past the give-up time", jstAt(10, 14, 10, 0, 0), jstAt(10, 14, 15, 59, 0), true},
		{"same session, 5h01m past the give-up time", jstAt(10, 14, 10, 0, 0), jstAt(10, 14, 16, 1, 0), false},
		{"Friday decision, Monday 14:29 (give-up time 09:30)", jstAt(10, 16, 15, 0, 0), jstAt(10, 19, 14, 29, 0), true},
		{"Friday decision, Monday 14:31", jstAt(10, 16, 15, 0, 0), jstAt(10, 19, 14, 31, 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := openDB(t)
			store := disclosureskip.NewStore(conn)
			ev := disclosureskip.NewEvaluator(store, weekdaysOnly, disclosureskip.WithEvaluatorClock(func() time.Time { return tc.restart }))
			insertDecision(t, conn, 1, "trader", "LONG", tc.decided)
			if err := ev.Cycle(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, ok := recordOf(t, conn, 1); ok != tc.want {
				t.Fatalf("recorded = %v, want %v", ok, tc.want)
			}
		})
	}
}
