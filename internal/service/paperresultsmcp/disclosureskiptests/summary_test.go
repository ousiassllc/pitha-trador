package disclosureskiptests

import (
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/disclosureskipdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func near(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

// The tool counts skip / no_skip / unknown and the decisions not recorded yet,
// and the label results of each group, over the live cohort's entry candidates
// of the period.
func TestSummaryCountsVerdictsAndLabels(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, path, "").call(t, args(nil)).data(t)

	if want := (disclosureskipdiag.Population{EntryCandidates: 8, Recorded: 7, NotRecorded: 1}); d.Population != want {
		t.Errorf("population = %+v, want %+v", d.Population, want)
	}
	v := d.Verdicts
	if v.Skip.Count != 2 || v.Skip.Labeled != 2 || v.Skip.Unlabeled != 0 || v.Skip.Wins != 1 || !near(v.Skip.WinRate, 0.5) ||
		v.Skip.NetReturn == nil || !near(&v.Skip.NetReturn.Mean, 10) || !near(&v.Skip.NetReturn.Sum, 20) {
		t.Errorf("skip = %+v", v.Skip)
	}
	if v.NoSkip.Count != 3 || v.NoSkip.Labeled != 3 || v.NoSkip.Unlabeled != 0 || v.NoSkip.Wins != 1 || !near(v.NoSkip.WinRate, 1.0/3) ||
		v.NoSkip.NetReturn == nil || !near(&v.NoSkip.NetReturn.Mean, -20.0/3) || !near(&v.NoSkip.NetReturn.Sum, -20) {
		t.Errorf("no_skip = %+v", v.NoSkip)
	}
	if v.Unknown.Count != 2 || v.Unknown.Labeled != 1 || v.Unknown.Unlabeled != 1 || v.Unknown.Wins != 1 || !near(v.Unknown.WinRate, 1) ||
		v.Unknown.NetReturn == nil || !near(&v.Unknown.NetReturn.Sum, 5) {
		t.Errorf("unknown = %+v", v.Unknown)
	}
	if want := []disclosureskipdiag.Bucket{{Code: "incomplete_fetch", Count: 1}, {Code: "no_fetch", Count: 1}}; !slices.Equal(d.UnknownReasons, want) {
		t.Errorf("unknown reasons = %+v, want %+v", d.UnknownReasons, want)
	}
	if want := []disclosureskipdiag.Bucket{{Code: "earnings", Count: 1}, {Code: "forecast_revision", Count: 2}}; !slices.Equal(d.SkipKinds, want) {
		t.Errorf("skip kinds = %+v, want %+v", d.SkipKinds, want)
	}
	if !slices.Equal(d.Rule.Versions, []int{1}) || d.Labels.LabelVersion != 2 || d.Labels.Win == "" {
		t.Errorf("rule/labels = %+v / %+v", d.Rule, d.Labels)
	}
	if !slices.Equal(d.PromptVersions.Excluded, []string{"trader-v4", "trader-v5"}) {
		t.Errorf("excluded versions = %v", d.PromptVersions.Excluded)
	}
}

// A group without a labeled decision has a null win rate and net return, not zero.
func TestSummaryEmptyGroupsAreNullNotZero(t *testing.T) {
	path, w := seed(t)
	testkit.MustExec(t, w, `DELETE FROM disclosure_skip_records WHERE verdict = 'skip'`)
	d := newTestServer(t, path, "").call(t, args(nil)).data(t)
	if g := d.Verdicts.Skip; g.Count != 0 || g.WinRate != nil || g.NetReturn != nil {
		t.Errorf("empty skip group = %+v, want zero counts and null rate/return", g)
	}
	if d.Population.NotRecorded != 3 { // decisions 1 and 2 lost their records, 8 never had one
		t.Errorf("population = %+v", d.Population)
	}
	if len(d.SkipKinds) != 0 {
		t.Errorf("skip kinds = %+v, want none", d.SkipKinds)
	}
}

// prompt_version restricts the counts to one version; the default leaves the
// other cohort's rows out.
func TestSummaryPromptVersionScope(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, path, "").call(t, args(map[string]any{"prompt_version": "trader-v4"})).data(t)
	if d.PromptVersions.Only != "trader-v4" || d.Population.EntryCandidates != 1 || d.Verdicts.Skip.Count != 1 || !near(&d.Verdicts.Skip.NetReturn.Sum, 900) {
		t.Errorf("trader-v4 scope: %+v / %+v / %+v", d.PromptVersions, d.Population, d.Verdicts.Skip)
	}
}

func TestSummaryRejectsBadArguments(t *testing.T) {
	path, _ := seed(t)
	srv := newTestServer(t, path, "")
	for name, a := range map[string]any{
		"unknown field":       args(map[string]any{"min_entry_quality": []string{"good"}}),
		"long prompt version": args(map[string]any{"prompt_version": strings.Repeat("v", 200)}),
		"only from":           map[string]any{"from": "2026-10-10"},
	} {
		if r := srv.call(t, a); r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, body %s; want 422", name, r.status, r.body)
		}
	}
}

// The tool reads through the read-only connection: only SELECTs are issued, no
// table changes, a write on the connection fails, and nothing the recorder
// stores beyond counts (titles, URLs, responses) is returned.
func TestSummaryIsReadOnlyAndReturnsOnlyCounts(t *testing.T) {
	path, w := seed(t)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, path, driverName)
	tables := []string{"disclosure_skip_records", "disclosure_skip_matches", "disclosure_items", "disclosure_fetches", "jev_decisions", "calibration_outcomes"}
	count := func() map[string]int {
		out := map[string]int{}
		for _, table := range tables {
			var n int
			if err := w.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			out[table] = n
		}
		return out
	}
	before := count()
	body := srv.call(t, args(nil)).raw(t)
	for _, leak := range []string{"SENTINEL-TITLE", "SENTINEL-URL", "state_json", "response_json", "document_url", "title"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("the response contains %q", leak)
		}
	}
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO disclosure_skip_records (jev_decision_id, symbol, decided_at, verdict, window_from, rule_version, evaluated_at) VALUES (99, 'x', 't', 'skip', 't', 1, 't')`,
		`UPDATE disclosure_skip_records SET verdict = 'unknown'`,
		`DELETE FROM disclosure_skip_matches`,
		`UPDATE jev_decisions SET direction = 'NONE'`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("a write succeeded on the MCP connection: %s", testkit.FirstLine(q))
		}
	}
	if after := count(); !equalCounts(before, after) {
		t.Fatalf("table row counts changed: %v -> %v", before, after)
	}
	forbidden := []string{"INSERT", "UPDATE ", "DELETE", "CREATE", "DROP", "ALTER", "ATTACH", "VACUUM", "state_json", "response_json", "document_url", "title", "SELECT *"}
	for _, stmt := range counter.StatementLog() {
		trimmed := strings.TrimSpace(stmt)
		if !strings.HasPrefix(trimmed, "SELECT") && !strings.HasPrefix(trimmed, "PRAGMA") {
			continue // the test's own rejected writes above
		}
		for _, f := range forbidden {
			if strings.Contains(trimmed, f) {
				t.Errorf("statement names %q: %s", f, testkit.FirstLine(stmt))
			}
		}
	}
}

func equalCounts(a, b map[string]int) bool {
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return len(a) == len(b)
}
