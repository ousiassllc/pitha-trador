package metalabeltests

import (
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// seedVersion seeds the decisions of one prompt version, ids from firstID, one
// at each of the given times. A decision whose index is in noLabel has no
// triple-barrier outcome and one in flat has direction NONE.
func seedVersion(t *testing.T, w *sql.DB, version string, firstID int, at []time.Time, noLabel, flat map[int]bool) {
	t.Helper()
	for i, d := range learnable(len(at), firstID, at[len(at)-1], "jev-1.13.0") {
		d.at = at[i]
		if noLabel[i] {
			d.label = 0
		}
		if flat[i] {
			d.direction = "NONE"
		}
		insertDecision(t, w, d)
		testkit.MustExec(t, w, `UPDATE jev_decisions SET question_version = ? WHERE id = ?`, version, d.id)
	}
}

func at(day string, hours ...int) []time.Time {
	d, _ := time.Parse(time.DateOnly, day)
	out := make([]time.Time, len(hours))
	for i, h := range hours {
		out[i] = d.Add(time.Duration(h) * time.Hour)
	}
	return out
}

// Issue #913: with trader-v4 live, the trader-v3 decisions the retraining
// leaves out are counted per version (all, LONG/SHORT, with a label) with the
// JST dates of the first and last one, together with the live version and the
// newest promotion, so a "no training rows" caused by the cohort is told from
// one caused by the cutoff. The funnel itself still counts the live cohort only.
func TestReadinessShowsWhatTheOtherCohortHolds(t *testing.T) {
	now := testkit.FixedNow() // 2026-10-18T03:00Z
	path, w := readinessDB(t)
	// trader-v3: 5 decisions; the 1st at 2026-10-01 20:00 UTC = 10/02 05:00 JST
	// (the date is the JST one), one is NONE and one has no label.
	seedVersion(t, w, "trader-v3", 1, append(at("2026-10-01", 20), append(at("2026-10-05", 1, 2, 3), at("2026-10-13", 14)...)...),
		map[int]bool{2: true}, map[int]bool{1: true})
	// trader-v2 is in the same cohort as trader-v3 and also left out.
	seedVersion(t, w, "trader-v2", 20, at("2026-10-03", 1), nil, nil)
	// trader-v4 is live: these are the rows the funnel counts.
	seedVersion(t, w, "trader-v4", 40, at("2026-10-14", 1, 2, 3), nil, nil)
	testkit.Setting(t, w, jevshadowsettings.KeyLiveVersion, `"trader-v4"`)
	testkit.MustExec(t, w, `INSERT INTO jev_prompt_promotions (at, actor, client, from_version, to_version) VALUES (?, 'gui', '', 'trader-v2', 'trader-v3')`,
		testkit.Stored(t, "2026-09-01T00:00:00Z"))
	testkit.MustExec(t, w, `INSERT INTO jev_prompt_promotions (at, actor, client, from_version, to_version) VALUES (?, 'mcp', '192.0.2.1', 'trader-v3', 'trader-v4')`,
		testkit.Stored(t, "2026-10-14T00:00:00Z"))

	d := callReadiness(t, newTestServer(t, path, now), fullLookback)

	if got := funnelRows(d); got[0] != 3 {
		t.Errorf("funnel rows = %v, want the 3 trader-v4 decisions only", got)
	}
	c := d.PromptCohort
	if c.LivePromptVersion != "trader-v4" {
		t.Errorf("live prompt version = %q", c.LivePromptVersion)
	}
	if p := c.LatestPromotion; p == nil || p.At != "2026-10-14T00:00:00Z" || p.FromVersion != "trader-v3" || p.ToVersion != "trader-v4" {
		t.Errorf("latest promotion = %+v, want trader-v3 -> trader-v4 at 2026-10-14T00:00:00Z (the newest row, not the older one)", p)
	}
	type row struct {
		version                         string
		all, longShort, labeled         int64
		oldest, newest, cohortOfVersion string
	}
	var got []row
	for _, e := range c.Excluded {
		got = append(got, row{e.PromptVersion, e.TraderDecisions, e.LongShortRows, e.LabelV2Rows, e.OldestDecisionDate, e.NewestDecisionDate, e.Cohort})
	}
	want := []row{
		// the oldest first (by first decision): trader-v3 from 10/02 (JST), then trader-v2 on 10/03
		{"trader-v3", 5, 4, 3, "2026-10-02", "2026-10-13", "entry-quality-v3"},
		{"trader-v2", 1, 1, 1, "2026-10-03", "2026-10-03", "entry-quality-v3"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("excluded = %+v\nwant %+v", got, want)
	}
}

// Nothing is left out when the live version's cohort is the only one in the
// window, and an excluded version with no decision in the read window is not
// listed (it is still named in excluded_prompt_versions). Without a promotion
// there is none to show.
func TestReadinessCohortIsEmptyWhenNothingIsLeftOut(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	seedVersion(t, w, "trader-v3", 1, at("2026-10-14", 1, 2, 3), nil, nil)
	// a decision older than the 90-day lookback is not in the read window
	seedVersion(t, w, "trader-v4", 10, at("2026-06-01", 1), nil, nil)

	srv := newTestServer(t, path, now)
	d := callReadiness(t, srv, fullLookback)
	if d.PromptCohort.LivePromptVersion != "trader-v3" || len(d.PromptCohort.Excluded) != 0 || d.PromptCohort.LatestPromotion != nil {
		t.Errorf("prompt cohort = %+v, want live trader-v3, nothing excluded, no promotion", d.PromptCohort)
	}
	if !slices.Equal(d.ExcludedPromptVersions, []string{"trader-v4", "trader-v5"}) {
		t.Errorf("excluded_prompt_versions = %v, want the name of the version the retraining leaves out", d.ExcludedPromptVersions)
	}
	if body := srv.call(t, "metalabel_training_readiness", fullLookback).body; !strings.Contains(string(body), `\"excluded\":[]`) {
		t.Errorf("the excluded list is not an empty array: %s", body)
	}
}
