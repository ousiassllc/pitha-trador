package trialtests

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
)

// skip records that arm was not asked about live decision id (created at ts, RFC3339 UTC).
func skip(t *testing.T, w *sql.DB, id int, arm, reason, ts string) {
	t.Helper()
	at, err := judgementTime(ts)
	if err != nil {
		t.Fatal(err)
	}
	if err := judgement.NewTrialRepository(w).InsertSkip(t.Context(), jevtrial.Skip{JevDecisionID: int64(id), ArmID: arm, Reason: reason, CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
}

func armSkips(t *testing.T, list []trialquery.ArmSkips, arm string) trialquery.ArmSkips {
	t.Helper()
	for _, a := range list {
		if a.ArmID == arm {
			return a
		}
	}
	t.Fatalf("no arm %s in %+v", arm, list)
	return trialquery.ArmSkips{}
}

// addSkips adds to seedMain's data: decision 7 (asked of arm-a only) was skipped for arm-b (slots full) and arm-c
// (arm cap); decision 9 (in the period) has an arm-a row and no skip record; arm-x was only ever skipped
// (no key) on decisions 2 and 3; decision 8 (August, outside the period) has a skip that must not count.
func addSkips(t *testing.T, w *sql.DB) {
	t.Helper()
	liveDecision(t, w, 9, "2026-10-01T09:00:00Z", "LONG")
	insertRow(t, w, row{decision: 9, arm: armA})
	skip(t, w, 7, armB, jevtrial.SkipConcurrencyFull, "2026-10-01T07:00:01Z")
	skip(t, w, 7, armC, jevtrial.SkipDailyCapArm, "2026-10-01T07:00:01Z")
	skip(t, w, 2, "arm-x", jevtrial.SkipKeyNotSet, "2026-10-01T02:00:01Z")
	skip(t, w, 3, "arm-x", jevtrial.SkipKeyNotSet, "2026-10-01T03:00:01Z")
	skip(t, w, 8, armB, jevtrial.SkipMinInterval, "2026-08-01T00:00:01Z")
}

// The skip counts sit next to the cohort premise, by reason with every reason present, and change neither
// the cohort nor any arm's missing rate.
func TestCompareShowsSkippedDecisionsNextToTheCohort(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	before := compare(t, srv, period)
	addSkips(t, w)
	c := compare(t, srv, period)

	// Skips are not rows: the cohort, the rows and every missing rate are as before.
	if c.Cohort.Size != before.Cohort.Size || c.Cohort.Size != 6 {
		t.Fatalf("cohort size = %d (before %d), want 6: a skip must not change the cohort", c.Cohort.Size, before.Cohort.Size)
	}
	for _, arm := range []string{armB, armC} {
		got, want := armReport(t, c, arm).PeriodRows, armReport(t, before, arm).PeriodRows
		if got.Total != want.Total || got.Failed != want.Failed || got.Refused != want.Refused || got.Partial != want.Partial {
			t.Errorf("%s rows = %+v, want %+v: a skip is not a success or a failure row", arm, got, want)
		}
		if g, w := armReport(t, c, arm).PeriodRows.MissingRate, want.MissingRate; (g == nil) != (w == nil) || (g != nil && *g != *w) {
			t.Errorf("%s missing rate changed with skips: %v -> %v", arm, w, g)
		}
	}

	s := c.Fairness.Skipped
	if s.CountedInMissingRate {
		t.Error("counted_in_missing_rate must be false")
	}
	if s.CohortDecisions != 6 || s.DroppedDecisions != 1 || s.OutsideCohortWithoutSkip != 1 || s.RecordedDecisions != 8 {
		t.Errorf("decisions = recorded %d cohort %d dropped %d without skip %d, want 8/6/1/1 (decision 7 is dropped, decision 9 only has an arm-a row)",
			s.RecordedDecisions, s.CohortDecisions, s.DroppedDecisions, s.OutsideCohortWithoutSkip)
	}
	if s.RecordedDecisions != s.CohortDecisions+s.DroppedDecisions+s.OutsideCohortWithoutSkip {
		t.Errorf("recorded %d != cohort + dropped + outside without skip", s.RecordedDecisions)
	}
	if s.ArmDecisionsSkipped != 2 || s.ByReason[jevtrial.SkipConcurrencyFull] != 1 || s.ByReason[jevtrial.SkipDailyCapArm] != 1 || s.ByReason[jevtrial.SkipMinInterval] != 0 {
		t.Errorf("by reason = %v (total %d), want one concurrency_full and one daily_cap_arm: the August skip is outside the period", s.ByReason, s.ArmDecisionsSkipped)
	}
	for _, reason := range jevtrial.SkipReasons {
		if _, ok := s.ByReason[reason]; !ok {
			t.Errorf("reason %s is missing from by_reason: an absent reason must read as 0", reason)
		}
	}
	b := armSkips(t, s.PerArm, armB)
	if b.Skipped != 1 || b.Asked != 6 || b.ByReason[jevtrial.SkipConcurrencyFull] != 1 {
		t.Errorf("arm-b skips = %+v", b)
	}
	near(t, "arm-b asked_share", b.AskedShare, 6.0/7.0)
	if a := armSkips(t, s.PerArm, armA); a.Skipped != 0 || a.Asked != 8 {
		t.Errorf("arm-a skips = %+v", a)
	}
	// Arms with skips only are shown, not hidden, and do not change the cohort.
	x := armSkips(t, s.ArmsWithoutRows, "arm-x")
	if x.Skipped != 2 || x.Asked != 0 || x.ByReason[jevtrial.SkipKeyNotSet] != 2 {
		t.Errorf("arm-x = %+v", x)
	}
	if got := strings.Join(c.Fairness.Caveats, ","); !strings.Contains(got, "skipped_decisions") {
		t.Errorf("caveats = %s, want skipped_decisions", got)
	}
	for _, key := range []string{"cohort", "missing_rate", "skipped", "dropped_decisions"} {
		if c.Definitions[key] == "" {
			t.Errorf("definition %q is missing", key)
		}
	}
	if !strings.Contains(c.Definitions["missing_rate"], "見送り") || !strings.Contains(c.Definitions["cohort"], "見送った判断") {
		t.Errorf("the cohort and missing_rate definitions must name the skips: %q / %q", c.Definitions["cohort"], c.Definitions["missing_rate"])
	}
}

// With the arms chosen the skips of other arms are not counted, and the arm-only-skipped list is empty.
func TestCompareSkippedFollowsTheChosenArms(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	addSkips(t, w)
	c := compare(t, srv, withArms(armA, armB))
	s := c.Fairness.Skipped
	if s.ArmDecisionsSkipped != 1 || s.ByReason[jevtrial.SkipConcurrencyFull] != 1 || s.ByReason[jevtrial.SkipDailyCapArm] != 0 {
		t.Errorf("skipped = %+v, want only arm-b's skip", s)
	}
	if len(s.PerArm) != 2 || len(s.ArmsWithoutRows) != 0 {
		t.Errorf("per_arm %d, arms_without_rows %d", len(s.PerArm), len(s.ArmsWithoutRows))
	}
	// Decision 7 is dropped (arm-b skipped it), decision 9 has no arm-b row and no skip.
	if s.DroppedDecisions != 1 || s.OutsideCohortWithoutSkip != 1 || s.RecordedDecisions != s.CohortDecisions+2 {
		t.Errorf("decisions = %+v", s)
	}
}

// Without any skip every count is 0 and nothing is hidden or invented.
func TestCompareSkippedIsZeroWithoutSkips(t *testing.T) {
	srv, w := newServer(t)
	seedMain(t, w)
	s := compare(t, srv, period).Fairness.Skipped
	if s.ArmDecisionsSkipped != 0 || s.DroppedDecisions != 0 || len(s.ArmsWithoutRows) != 0 || len(s.ByReason) != len(jevtrial.SkipReasons) {
		t.Errorf("skipped = %+v", s)
	}
	if s.CohortDecisions != 6 || s.OutsideCohortWithoutSkip != 1 || s.RecordedDecisions != 7 {
		t.Errorf("decisions = %+v, want cohort 6, 1 asked of arm-a only (decision 7), 7 recorded", s)
	}
	empty := compare(t, srv, map[string]any{"from": "2026-09-01", "to": "2026-09-05"}).Fairness.Skipped
	if empty.RecordedDecisions != 0 || empty.PerArm == nil || empty.ArmsWithoutRows == nil {
		t.Errorf("empty period = %+v", empty)
	}
}

// jev_trial_cost shows today's skips by reason next to the caps: overall and per configured arm; they are not calls
// and carry no cost.
func TestCostShowsTodaysSkips(t *testing.T) {
	srv, w := newServer(t)
	seedCost(t, w)
	before := cost(t, srv, map[string]any{})
	liveDecision(t, w, 90, "2026-10-18T01:00:00Z", "LONG")
	liveDecision(t, w, 91, "2026-10-18T02:00:00Z", "LONG")
	skip(t, w, 90, armA, jevtrial.SkipDailyCapOverall, "2026-10-18T01:00:01Z")
	skip(t, w, 91, armA, jevtrial.SkipDailyCapOverall, "2026-10-18T02:00:01Z")
	skip(t, w, 90, armB, jevtrial.SkipThrottled, "2026-10-18T01:00:01Z")
	skip(t, w, 91, armB, jevtrial.SkipMinInterval, "2026-10-17T01:00:01Z") // yesterday
	c := cost(t, srv, map[string]any{})
	if c.Total.Calls != before.Total.Calls || c.Total.CountedTotalUSD != before.Total.CountedTotalUSD {
		t.Errorf("skips changed the calls or the counted cost: %+v vs %+v", c.Total, before.Total)
	}
	l := c.Limits
	if l.SkippedToday[jevtrial.SkipDailyCapOverall] != 2 || l.SkippedToday[jevtrial.SkipThrottled] != 1 || l.SkippedToday[jevtrial.SkipMinInterval] != 0 {
		t.Errorf("skipped today = %v", l.SkippedToday)
	}
	for _, a := range l.Arms {
		switch a.ArmID {
		case armA:
			if a.SkippedToday[jevtrial.SkipDailyCapOverall] != 2 {
				t.Errorf("arm-a skipped today = %v", a.SkippedToday)
			}
		case armB:
			if a.SkippedToday[jevtrial.SkipThrottled] != 1 || a.SkippedToday[jevtrial.SkipMinInterval] != 0 {
				t.Errorf("arm-b skipped today = %v", a.SkippedToday)
			}
		}
		if len(a.SkippedToday) != len(jevtrial.SkipReasons) {
			t.Errorf("%s: every reason must be present, got %v", a.ArmID, a.SkippedToday)
		}
	}
}
