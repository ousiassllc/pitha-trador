package trialtests

import (
	"database/sql"
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/trialquery"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// seedCost plants cost rows on two JST days (10-17 and 10-18, "today" of the test clock).
//
//	10-17  arm-a reported 0.01                          arm-b reported 0.003
//	10-18  arm-a reported 0.02, list_price_estimate 0.005 (counted 0.024: the caps count it at the worst-case price)
//	10-18  arm-b failed, unknown cost, counted 0.0048 (worst-case price)
//	10-18  arm-b failed, unknown cost, counted 0 (an HTTP error response, assumed not billed)
func seedCost(t *testing.T, w *sql.DB) {
	t.Helper()
	rows := []row{
		{decision: 101, arm: armA, source: "reported", cost: f64(0.01), counted: 0.01, created: "2026-10-17T01:00:00Z"},
		{decision: 102, arm: armB, source: "reported", cost: f64(0.003), counted: 0.003, created: "2026-10-17T02:00:00Z"},
		{decision: 103, arm: armA, source: "reported", cost: f64(0.02), counted: 0.02, created: "2026-10-18T01:00:00Z"},
		{decision: 104, arm: armA, source: "list_price_estimate", cost: f64(0.005), counted: 0.024, created: "2026-10-18T01:30:00Z"},
		{decision: 105, arm: armB, status: "failed", failure: "timeout", counted: 0.0048, created: "2026-10-18T02:00:00Z"},
		{decision: 106, arm: armB, status: "failed", failure: "http_402", counted: 0, created: "2026-10-18T02:30:00Z"},
	}
	for _, r := range rows {
		insertRow(t, w, r)
	}
	arms, err := jevtrialsettings.EncodeArms([]jevtrialsettings.Arm{
		{ID: armA, Provider: "openrouter", Model: "vendor/arm-a", PromptVersion: "trader-v3", DailyCostCapUSD: 0.05, ListPriceUSDPerMtok: 0.24},
		{ID: armB, Provider: "openrouter", Model: "vendor/arm-b", PromptVersion: "trader-v3", DailyCostCapUSD: 0.02},
	})
	if err != nil {
		t.Fatal(err)
	}
	testkit.Setting(t, w, jevtrialsettings.KeyEnabled, "1")
	testkit.Setting(t, w, jevtrialsettings.KeyArms, arms)
	testkit.Setting(t, w, jevtrialsettings.KeyDailyCostCapUSD, "0.2")
	testkit.Setting(t, w, jevtrialsettings.KeyHalt, `{"date":"2026-10-18","reason":"payment_required","status":402,"at":"2026-10-18T02:30:00Z"}`)
	testkit.Setting(t, w, jevtrialsettings.HaltKey(armB), `{"date":"2026-10-17","reason":"forbidden","status":403,"at":"2026-10-17T02:30:00Z"}`)
	testkit.Setting(t, w, jevtrialsettings.FailuresKey(armB), `{"date":"2026-10-18","count":2}`)
	testkit.Setting(t, w, jevtrialsettings.ThrottleKey(armB), `{"date":"2026-10-18","count":1,"status":429,"reason":"api_rate_limited","at":"2026-10-18T02:00:00Z","until":"2026-10-18T02:05:00Z"}`)
}

func closeTo(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func cost(t *testing.T, srv *testServer, args map[string]any) trialquery.CostReport {
	t.Helper()
	var c trialquery.CostReport
	srv.call(t, "jev_trial_cost", args).data(t, "jev_trial_cost", &c)
	return c
}

func armCost(t *testing.T, arms []trialquery.ArmCost, id string) trialquery.ArmCost {
	t.Helper()
	for _, a := range arms {
		if a.ArmID == id {
			return a
		}
	}
	t.Fatalf("no arm %s in %+v", id, arms)
	return trialquery.ArmCost{}
}

// Cost is split by source and by arm. An unknown cost is never 0 and never added to the known sums.
func TestCostSplitsBySourceAndNeverZeroesUnknown(t *testing.T) {
	srv, w := newServer(t)
	seedCost(t, w)
	c := cost(t, srv, map[string]any{})

	tot := c.Total
	if tot.Calls != 6 || tot.Reported.Calls != 3 || tot.ListPriceEstimate.Calls != 1 || tot.Unknown.Calls != 2 {
		t.Fatalf("total = %+v", tot)
	}
	closeTo(t, "reported usd", tot.Reported.USD, 0.033)
	closeTo(t, "estimate usd", tot.ListPriceEstimate.USD, 0.005)
	closeTo(t, "unknown_counted_usd", tot.Unknown.UnknownCountedUSD, 0.0048)
	// The caps counted the list-price estimate at the worst-case price, so counted_usd is above usd.
	closeTo(t, "reported counted_usd", tot.Reported.CountedUSD, 0.033)
	closeTo(t, "estimate counted_usd", tot.ListPriceEstimate.CountedUSD, 0.024)
	closeTo(t, "counted_total_usd", tot.CountedTotalUSD, 0.033+0.024+0.0048) // the cap basis includes the unknown's counted amount
	if tot.Unknown.ActualUSD != nil || tot.Unknown.Basis != trialquery.UnknownBasis || tot.Unknown.CountedAsZeroCalls != 1 {
		t.Errorf("unknown = %+v", tot.Unknown)
	}

	// The raw JSON says null, not 0, for the unknown cost, and the figure the caps counted is named as such.
	var raw struct {
		Total struct {
			Unknown map[string]any `json:"unknown"`
		} `json:"total"`
	}
	srv.call(t, "jev_trial_cost", map[string]any{}).data(t, "jev_trial_cost", &raw)
	if v, ok := raw.Total.Unknown["actual_usd"]; !ok || v != nil {
		t.Errorf("unknown.actual_usd = %v (present %v), want null", v, ok)
	}
	if raw.Total.Unknown["basis"] != "cap_counting_upper_bound" {
		t.Errorf("unknown.basis = %v", raw.Total.Unknown["basis"])
	}

	if len(c.PerArm) != 2 {
		t.Fatalf("per_arm = %+v", c.PerArm)
	}
	a, b := armCost(t, c.PerArm, armA), armCost(t, c.PerArm, armB)
	closeTo(t, "arm-a reported", a.Reported.USD, 0.03)
	if a.Unknown.Calls != 0 || a.Unknown.ActualUSD != nil {
		t.Errorf("arm-a unknown = %+v", a.Unknown)
	}
	closeTo(t, "arm-b reported", b.Reported.USD, 0.003)
	closeTo(t, "arm-b unknown_counted", b.Unknown.UnknownCountedUSD, 0.0048)

	if len(c.Daily) != 2 || c.Daily[0].Date != "2026-10-17" || c.Daily[1].Date != "2026-10-18" {
		t.Fatalf("daily = %+v", c.Daily)
	}
	d17, d18 := c.Daily[0], c.Daily[1]
	closeTo(t, "10-17 reported", d17.Overall.Reported.USD, 0.013)
	if d17.Overall.Unknown.Calls != 0 || d18.Overall.Calls != 4 || d18.Overall.Unknown.Calls != 2 {
		t.Errorf("daily split: 10-17 %+v / 10-18 %+v", d17.Overall, d18.Overall)
	}
	closeTo(t, "10-18 counted total", d18.Overall.CountedTotalUSD, 0.02+0.024+0.0048)
	closeTo(t, "10-18 arm-a estimate", armCost(t, d18.Arms, armA).ListPriceEstimate.USD, 0.005)
}

// The period and the arm list narrow the figures.
func TestCostPeriodAndArmFilters(t *testing.T) {
	srv, w := newServer(t)
	seedCost(t, w)
	one := cost(t, srv, map[string]any{"from": "2026-10-18", "to": "2026-10-19", "arms": []string{armB}})
	if one.Total.Calls != 2 || len(one.PerArm) != 1 || len(one.Daily) != 1 || one.Total.Reported.Calls != 0 || one.Total.Unknown.Calls != 2 {
		t.Errorf("arm-b on 10-18 = %+v", one.Total)
	}
	if len(one.Limits.Arms) != 1 || one.Limits.Arms[0].ArmID != armB {
		t.Errorf("limits arms = %+v", one.Limits.Arms)
	}
	empty := cost(t, srv, map[string]any{"from": "2026-09-01", "to": "2026-09-05"})
	if empty.Total.Calls != 0 || len(empty.Daily) != 0 || empty.Total.Unknown.ActualUSD != nil {
		t.Errorf("empty period = %+v", empty.Total)
	}
}

// The caps and the stop state are read from runtime_settings (reason and day only).
func TestCostReadsCurrentCapsAndStops(t *testing.T) {
	srv, w := newServer(t)
	seedCost(t, w)
	l := cost(t, srv, map[string]any{}).Limits
	if !l.Enabled || l.Today != "2026-10-18" || l.ArmsConfigInvalid {
		t.Errorf("limits = %+v", l)
	}
	closeTo(t, "overall cap", l.OverallDailyCapUSD, 0.2)
	if l.OverallCapSource != "stored" || l.Halt == nil || l.Halt.Reason != "payment_required" || !l.Halt.ActiveToday {
		t.Errorf("overall = %s %+v", l.OverallCapSource, l.Halt)
	}
	closeTo(t, "overall counted today", l.OverallCountedTodayUSD, 0.02+0.024+0.0048)
	if l.FailureCap != jevtrialsettings.DailyFailureCap || l.WorstCaseUSDPerMtok != jevtrialsettings.WorstCaseUSDPerMtok || len(l.Arms) != 2 {
		t.Errorf("limits = %+v", l)
	}
	a, b := l.Arms[0], l.Arms[1]
	closeTo(t, "arm-a cap", a.DailyCapUSD, 0.05)
	closeTo(t, "arm-a counted today", a.CountedTodayUSD, 0.02+0.024)
	if !a.ListPriceKnown || a.Halt != nil || a.Throttle != nil {
		t.Errorf("arm-a = %+v", a)
	}
	closeTo(t, "arm-b cap", b.DailyCapUSD, 0.02)
	closeTo(t, "arm-b counted today", b.CountedTodayUSD, 0.0048)
	if b.ListPriceKnown || b.FailuresToday != 2 || b.Halt == nil || b.Halt.Reason != "forbidden" || b.Halt.ActiveToday {
		t.Errorf("arm-b = %+v (the halt record is of yesterday)", b)
	}
	if b.Throttle == nil || b.Throttle.Reason != "api_rate_limited" || b.Throttle.Date != "2026-10-18" {
		t.Errorf("arm-b throttle = %+v", b.Throttle)
	}
}

// With nothing stored the trial is off, no arm is configured and the cap is the default.
func TestCostDefaultsAreOff(t *testing.T) {
	srv, _ := newServer(t)
	c := cost(t, srv, map[string]any{})
	l := c.Limits
	if l.Enabled || len(l.Arms) != 0 || l.Halt != nil || l.OverallCapSource != "default" || l.OverallDailyCapUSD != jevtrialsettings.DefaultDailyCostCapUSD {
		t.Errorf("limits = %+v", l)
	}
	if c.Total.Calls != 0 || len(c.PerArm) != 0 || len(c.Daily) != 0 {
		t.Errorf("cost = %+v", c)
	}
}
