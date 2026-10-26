package entryqualitytests

import (
	"database/sql"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/entryqualitydiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

const (
	eqBelow  = `entry_quality_below_threshold: entry_quality="poor" < min="good"`
	contFail = `continuation_probability_below_threshold: continuation_probability=0.5000 < min=0.6000`
	toxFail  = `toxic_flow_above_threshold: toxic_flow=0.5000 > max=0.3500`
	liqFail  = `liquidity_stressed_above_threshold: liquidity_stressed=0.3000 > max=0.2500`
)

// seed writes the decisions of one period, the way the engine left them with
// min_entry_quality = good (the Paper default): each signal's reject_reason is
// written by hand, so the expected counts below do not depend on the code under
// test. Rows 1-7 reached the entry guards; every other row left the tree
// somewhere else.
func seed(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for _, f := range []fixture{
		{id: 1, direction: "LONG", ans: answer{"poor", 0.7, 0.1, 0.1}, signal: eqBelow},
		{id: 2, direction: "LONG", ans: answer{"fair", 0.7, 0.1, 0.1}, signal: strings.Replace(eqBelow, `"poor"`, `"fair"`, 1)},
		{id: 3, direction: "LONG", ans: answer{"good", 0.7, 0.1, 0.1}, signal: ""},
		{id: 4, direction: "LONG", ans: answer{"good", 0.5, 0.1, 0.1}, signal: contFail},
		{id: 5, direction: "SHORT", ans: answer{"strong", 0.7, 0.5, 0.1}, signal: toxFail},
		{id: 6, direction: "SHORT", ans: answer{"exceptional", 0.7, 0.1, 0.3}, signal: liqFail},
		{id: 7, direction: "SHORT", ans: answer{"strong", 0.8, 0.1, 0.1}, signal: ""},
		// Turned away before entry_quality was looked at.
		{id: 8, direction: "LONG", ans: answer{"fair", 0.7, 0.1, 0.1}, signal: "spread_too_wide: spread_bps=40.00 > max=30.00"},
		{id: 9, direction: "SHORT", ans: answer{"good", 0.7, 0.1, 0.1}, signal: "not_lendable"},
		// The VWAP reversion rule: entry_quality does not apply.
		{id: 10, direction: "LONG", ans: answer{"good", 0.7, 0.1, 0.1}, abArm: "regime", regime: "mean_revert", signal: ""},
		{id: 11, direction: "LONG", response: `{"direction":"LONG","entry_quality":"good","continuation_probability":0.7,"liquidity_stressed":0.1,"confidence":0.7}`, signal: "missing_data"},
		// An unreadable answer whose signal is not a pre-guard reject.
		{id: 12, direction: "LONG", response: `not json`, signal: ""},
		{id: 13, direction: "LONG", ans: answer{"good", 0.7, 0.1, 0.1}, signal: "-"},
		// A NONE answer, another prompt cohort, and a decision outside the period.
		{id: 14, direction: "NONE", ans: answer{"poor", 0.1, 0.1, 0.1}, signal: "jev_none"},
		{id: 15, direction: "LONG", version: "trader-v4", ans: answer{"good", 0.7, 0.1, 0.1}, signal: ""},
		{id: 16, direction: "LONG", ans: answer{"poor", 0.7, 0.1, 0.1}, signal: eqBelow, at: "2026-10-01T01:00:00Z"},
	} {
		insertFixture(t, w, f)
	}
	return path, w
}

func counts(c entryqualitydiag.Counts) [7]int {
	return [7]int{c.Candidates, c.BelowEntryQuality, c.PassEntryQuality, c.BlockedContinuation, c.BlockedToxicFlow, c.BlockedLiquidityStressed, c.PassAllGuards}
}

func scenario(t *testing.T, d entryqualitydiag.Data, level string) entryqualitydiag.Scenario {
	t.Helper()
	for _, s := range d.Scenarios {
		if s.MinEntryQuality == level {
			return s
		}
	}
	t.Fatalf("no scenario %q in %+v", level, d.Scenarios)
	return entryqualitydiag.Scenario{}
}

// The population and the scenarios of a seeded period, against counts worked
// out by hand. Layout of counts: candidates, below entry_quality, pass
// entry_quality, blocked by continuation / toxic flow / liquidity, pass all.
func TestWhatIfCountsTheSeededPeriod(t *testing.T) {
	path, _ := seed(t)
	d := newTestServer(t, path).call(t, args(nil)).data(t)

	p := d.Population
	if p.TraderDecisions != 14 || p.LongShort != 13 || p.VWAPReversion != 1 || p.NotLendableShort != 1 ||
		p.RejectedBeforeGuards != 3 || p.IncompleteAnswer != 1 || p.ReachedGuards != 7 {
		t.Errorf("population = %+v", p)
	}
	if sum := p.VWAPReversion + p.NotLendableShort + p.RejectedBeforeGuards + p.IncompleteAnswer + p.ReachedGuards; sum != p.LongShort {
		t.Errorf("classes add up to %d, long_short is %d", sum, p.LongShort)
	}
	wantBefore := []entryqualitydiag.Bucket{{Reason: "missing_data", Count: 1}, {Reason: "no_signal", Count: 1}, {Reason: "spread_too_wide", Count: 1}}
	if !slices.Equal(p.RejectedBeforeGuardsByReason, wantBefore) {
		t.Errorf("rejected before guards = %+v, want %+v", p.RejectedBeforeGuardsByReason, wantBefore)
	}

	wantDist := []entryqualitydiag.Distribution{
		{PromptVersion: "trader-v3", EntryQuality: "poor", Count: 1}, {PromptVersion: "trader-v3", EntryQuality: "fair", Count: 2},
		{PromptVersion: "trader-v3", EntryQuality: "good", Count: 6}, {PromptVersion: "trader-v3", EntryQuality: "strong", Count: 2},
		{PromptVersion: "trader-v3", EntryQuality: "exceptional", Count: 1}, {PromptVersion: "trader-v3", EntryQuality: "unknown", Count: 1},
	}
	if !slices.Equal(d.Distribution, wantDist) {
		t.Errorf("distribution = %+v, want %+v", d.Distribution, wantDist)
	}
	if !slices.Equal(d.PromptVersions.Excluded, []string{"trader-v4"}) {
		t.Errorf("excluded versions = %v, want [trader-v4]", d.PromptVersions.Excluded)
	}

	for _, tc := range []struct {
		level              string
		total, long, short [7]int
	}{
		{"fair", [7]int{7, 1, 6, 1, 1, 1, 3}, [7]int{4, 1, 3, 1, 0, 0, 2}, [7]int{3, 0, 3, 0, 1, 1, 1}},
		{"good", [7]int{7, 2, 5, 1, 1, 1, 2}, [7]int{4, 2, 2, 1, 0, 0, 1}, [7]int{3, 0, 3, 0, 1, 1, 1}},
		{"strong", [7]int{7, 4, 3, 0, 1, 1, 1}, [7]int{4, 4, 0, 0, 0, 0, 0}, [7]int{3, 0, 3, 0, 1, 1, 1}},
	} {
		s := scenario(t, d, tc.level)
		if counts(s.Total) != tc.total || counts(s.Long) != tc.long || counts(s.Short) != tc.short {
			t.Errorf("min %s: total %v long %v short %v, want %v %v %v", tc.level, counts(s.Total), counts(s.Long), counts(s.Short), tc.total, tc.long, tc.short)
		}
	}
	if got := d.Scenarios[0].MinEntryQuality + d.Scenarios[1].MinEntryQuality + d.Scenarios[2].MinEntryQuality; got != "fairgoodstrong" {
		t.Errorf("scenarios are not the defaults in ascending order: %s", got)
	}
}

// At the minimum the decisions were made under (good), the tool's verdicts are
// the engine's recorded ones: the entry_quality count is the number of signals
// rejected for entry_quality, and every later guard's count is its signals'.
func TestWhatIfAtTheCurrentMinimumMatchesTheRecordedRejections(t *testing.T) {
	path, w := seed(t)
	d := newTestServer(t, path).call(t, args(map[string]any{"min_entry_quality": []string{"good"}})).data(t)
	if len(d.Scenarios) != 1 {
		t.Fatalf("scenarios = %+v, want just good", d.Scenarios)
	}
	got := d.Scenarios[0].Total

	// The recorded verdicts of the decisions that reached the guards (ids 1-7).
	recorded := map[string]int{}
	rows, err := w.Query(`SELECT substr(reject_reason, 1, instr(reject_reason || ':', ':') - 1), COUNT(*) FROM trade_signals
		WHERE jev_decision_id BETWEEN 1 AND 7 GROUP BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var head *string
		var n int
		if err := rows.Scan(&head, &n); err != nil {
			t.Fatal(err)
		}
		if head == nil {
			recorded["passed"] += n
			continue
		}
		recorded[*head] += n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]int{
		"entry_quality_below_threshold":            {got.BelowEntryQuality, recorded["entry_quality_below_threshold"]},
		"continuation_probability_below_threshold": {got.BlockedContinuation, recorded["continuation_probability_below_threshold"]},
		"toxic_flow_above_threshold":               {got.BlockedToxicFlow, recorded["toxic_flow_above_threshold"]},
		"liquidity_stressed_above_threshold":       {got.BlockedLiquidityStressed, recorded["liquidity_stressed_above_threshold"]},
		"passed":                                   {got.PassAllGuards, recorded["passed"]},
	} {
		if pair[0] != pair[1] || pair[1] == 0 {
			t.Errorf("%s: tool %d, recorded %d", name, pair[0], pair[1])
		}
	}
}

// A prompt_version restricts the counts to that version's rows; without it the
// rows of another cohort than the live version's stay out.
func TestWhatIfPromptVersionScope(t *testing.T) {
	seeded, _ := seed(t)
	srv := newTestServer(t, seeded)
	d := srv.call(t, args(map[string]any{"prompt_version": "trader-v4"})).data(t)
	if d.PromptVersions.Only != "trader-v4" || d.Population.TraderDecisions != 1 || d.Population.ReachedGuards != 1 {
		t.Errorf("trader-v4 scope: %+v / %+v", d.PromptVersions, d.Population)
	}
	if len(d.Distribution) != 1 || d.Distribution[0].PromptVersion != "trader-v4" {
		t.Errorf("distribution = %+v, want trader-v4 only", d.Distribution)
	}

	// With trader-v4 promoted, trader-v3 (and v1/v2) are the foreign cohort.
	path, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	insertFixture(t, w, fixture{id: 1, direction: "LONG", ans: answer{"good", 0.7, 0.1, 0.1}, signal: ""})
	insertFixture(t, w, fixture{id: 2, direction: "LONG", version: "trader-v4", ans: answer{"good", 0.7, 0.1, 0.1}, signal: ""})
	testkit.Setting(t, w, "jev.trader_prompt_version", `"trader-v4"`)
	live := newTestServer(t, path).call(t, args(nil)).data(t)
	if live.Population.TraderDecisions != 1 || len(live.Distribution) != 1 || live.Distribution[0].PromptVersion != "trader-v4" {
		t.Errorf("live v4: population %+v distribution %+v, want trader-v4 only", live.Population, live.Distribution)
	}
}

// The thresholds after entry_quality are today's policy.*: strategy.yaml with the
// runtime_settings overrides applied.
func TestWhatIfAppliesPolicyOverrides(t *testing.T) {
	path, w := seed(t)
	testkit.Setting(t, w, "policy.long.min_continuation_probability", `0.45`)
	d := newTestServer(t, path).call(t, args(map[string]any{"min_entry_quality": []string{"good"}})).data(t)

	if d.Thresholds.Long.MinContinuationProbability != 0.45 || d.Thresholds.Short.MinContinuationProbability != 0.6 {
		t.Errorf("thresholds = %+v", d.Thresholds)
	}
	if !slices.Equal(d.Thresholds.OverriddenKeys, []string{"policy.long.min_continuation_probability"}) {
		t.Errorf("overridden keys = %v", d.Thresholds.OverriddenKeys)
	}
	if long := d.Scenarios[0].Long; long.BlockedContinuation != 0 || long.PassAllGuards != 2 {
		t.Errorf("long at good with continuation 0.45: %+v, want row 4 to pass", long)
	}
}

func TestWhatIfRejectsBadArguments(t *testing.T) {
	path, _ := seed(t)
	srv := newTestServer(t, path)
	for name, a := range map[string]map[string]any{
		"unknown level":   {"min_entry_quality": []string{"great"}},
		"too many levels": {"min_entry_quality": []string{"poor", "fair", "good", "strong", "exceptional", "poor"}},
		"bad version":     {"prompt_version": "trader v4"},
		"unknown field":   {"threshold": 1},
	} {
		if r := srv.call(t, args(a)); r.status != http.StatusUnprocessableEntity || !strings.Contains(string(r.body), "invalid_argument") {
			t.Errorf("%s: status %d, body %s", name, r.status, r.body)
		}
	}
}
