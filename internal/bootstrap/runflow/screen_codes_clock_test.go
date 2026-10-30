package runflow_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// The seeded rows of "today" stay inside [00:00, 24:00) JST of the day the seed
// ran however close to midnight it ran, and the rows meant to be stale stay
// before it (issue #919: seeded as "d before now", the NONE signals fell into
// yesterday between 00:00 and about 01:00 JST, so the Home screen lost them).
// The clock is the argument of stamper, so the boundaries are tested directly.
func TestSeedStamp_KeepsTodaysRowsInTheJSTDay(t *testing.T) {
	oldestNone := time.Duration(30+len(mapKeys(labels.RejectReasons))) * time.Minute // the last NONE signal of seedEveryCode
	for _, now := range []time.Time{
		time.Date(2026, 10, 27, 0, 0, 0, 0, jst),
		time.Date(2026, 10, 27, 0, 10, 0, 0, jst),
		time.Date(2026, 10, 27, 23, 50, 0, 0, jst),
		time.Date(2026, 10, 27, 23, 59, 59, 999_000_000, jst),
	} {
		stamp := stamper(now)
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
		for name, d := range map[string]time.Duration{"newest": 0, "oldest NONE signal": oldestNone, "oldest row of today": seedTodayMaxAge} {
			at, err := time.Parse("2006-01-02T15:04:05.000Z", stamp(d))
			if err != nil {
				t.Fatalf("%s: stamp(%v) = %q: %v", now, d, stamp(d), err)
			}
			if at.Before(dayStart) || !at.Before(dayStart.AddDate(0, 0, 1)) {
				t.Errorf("seeded at %s: the %s is at %s, outside the JST day of %s", now.Format("15:04:05"), name, at.In(jst), dayStart.Format("2006-01-02"))
			}
		}
		at, err := time.Parse("2006-01-02T15:04:05.000Z", stamp(seedStalledAge))
		if err != nil {
			t.Fatalf("stamp(seedStalledAge): %v", err)
		}
		if !at.Before(dayStart) {
			t.Errorf("seeded at %s: the stalled proposals' updated_at is %s, not before the JST day of %s", now.Format("15:04:05"), at.In(jst), dayStart.Format("2006-01-02"))
		}
	}
}

// Whatever the time of day, the seeded proposals still in received/processing
// count as stalled, so the Home attention list names them: the wall-clock time
// does not change what the screens are checked on.
func TestScreens_HomeAttentionAlwaysListsTheStalledProposals(t *testing.T) {
	engine := newSeededEngine(t)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/home/attention", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /home/attention = %d", rec.Code)
	}
	if want := "処理が止まっている AI の注文提案"; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("GET /home/attention lost %q", want)
	}
}
