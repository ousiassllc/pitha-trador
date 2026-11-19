package promotiondiag

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

func days(pnls ...float64) []query.Day {
	out := make([]query.Day, len(pnls))
	for i, p := range pnls {
		out[i] = query.Day{PnlJPY: p, TradeCount: 1}
	}
	return out
}

func byName(items []Item) map[string]Item {
	m := map[string]Item{}
	for _, it := range items {
		m[it.Name] = it
	}
	return m
}

func judgedComparison() experiment.Comparison {
	c := experiment.Comparison{Verdict: experiment.VerdictImproved, Judged: true}
	lo, hi, diff := 5.0, 15.0, 10.0
	c.PerTradePnl.CILow, c.PerTradePnl.CIHigh, c.PerTradePnl.Diff = &lo, &hi, &diff
	c.Before.Performance.ClosedTradeCount, c.After.Performance.ClosedTradeCount = 400, 350
	c.Before.Performance.MaxDrawdownJPY, c.After.Performance.MaxDrawdownJPY = 100, 100
	return c
}

// A day-by-day win that rests on one day fails not_one_day_wins; a worse drawdown fails max_drawdown; both are
// judged only when the experiment itself was.
func TestSettingsSetDayAndDrawdownItems(t *testing.T) {
	c := judgedComparison()
	c.After.Performance.MaxDrawdownJPY = 101
	// before: 3 days of +10 (total 30, mean 10); after: one +100 day and two -5 days (total 90)
	items, n := settingsSetItems(c, days(10, 10, 10), days(100, -5, -5), 1, promotionsettings.Floor())
	m := byName(items)
	if it := m[ItemNotOneDayWins]; it.Status != StatusFail || it.Detail != DetailOneDayCarries || it.Actual.(float64) != -40 {
		t.Errorf("not_one_day_wins = %+v", it) // (90 - 100) - 30
	}
	if it := m[ItemPositiveDays]; it.Status != StatusFail || it.Actual.(float64) < 33 || it.Actual.(float64) > 34 {
		t.Errorf("positive_days = %+v", it) // one of three days above the before mean of 10
	}
	if it := m[ItemMaxDrawdown]; it.Status != StatusFail || it.Detail != DetailWorseDrawdown {
		t.Errorf("max_drawdown = %+v", it)
	}
	if n.BeforeDays != 3 || n.AfterDays != 3 || n.PositiveDayPct == nil {
		t.Errorf("numbers = %+v", n)
	}
	if len(items) != 11 {
		t.Fatalf("items = %d", len(items))
	}

	c.Verdict, c.Judged = experiment.VerdictConfounded, false
	items, _ = settingsSetItems(c, days(10, 10, 10), days(100, -5, -5), 1, promotionsettings.Floor())
	for _, name := range []string{ItemDiffCILower, ItemMultipleCompare, ItemPositiveDays, ItemNotOneDayWins, ItemMaxDrawdown} {
		if it := byName(items)[name]; it.Status != StatusUnknown || it.Detail != "experiment_confounded" {
			t.Errorf("%s = %+v, want unknown while the experiment is withheld", name, it)
		}
	}
	// the count and day floors are the checklist's own, whatever the experiment said
	if it := byName(items)[ItemCommonRows]; it.Status != StatusPass || it.Actual.(float64) != 350 {
		t.Errorf("common_rows = %+v", it)
	}
}

// No item of an experiment is a fail for want of what cannot apply, and no day means unknown (not a fail).
func TestSettingsSetNotApplicableAndNoDays(t *testing.T) {
	items, n := settingsSetItems(judgedComparison(), nil, nil, 1, promotionsettings.Floor())
	m := byName(items)
	for _, name := range []string{ItemSameRows, ItemAfterAICost, ItemAfterCutoff} {
		if it := m[name]; it.Status != StatusUnknown || !it.NotApplicable {
			t.Errorf("%s = %+v", name, it)
		}
	}
	for _, name := range []string{ItemPositiveDays, ItemNotOneDayWins} {
		if it := m[name]; it.Status != StatusUnknown || it.Detail != DetailNoTradeDays {
			t.Errorf("%s = %+v", name, it)
		}
	}
	if n.PositiveDayPct != nil || n.RestWithoutBestDay != nil {
		t.Errorf("numbers = %+v", n)
	}
}

// The Holm correction multiplies the experiment's p by the family and is capped at 1; no test statistic is unknown.
func TestSettingsSetHolmAdjustedP(t *testing.T) {
	c := judgedComparison()
	tv, df := 3.0, 100.0
	c.PerTradePnl.T, c.PerTradePnl.DF = &tv, &df
	_, one := settingsSetItems(c, days(1), days(1), 1, promotionsettings.Floor())
	_, many := settingsSetItems(c, days(1), days(1), 20, promotionsettings.Floor())
	if one.PValue == nil || *one.PValue <= 0 || *one.HolmAdjustedP != *one.PValue {
		t.Fatalf("family 1: %+v", one)
	}
	if want := min(1, *many.PValue*20); *many.HolmAdjustedP != want {
		t.Errorf("family 20: adjusted %v, want %v", *many.HolmAdjustedP, want)
	}
	items, none := settingsSetItems(judgedComparison(), days(1), days(1), 1, promotionsettings.Floor())
	if none.HolmAdjustedP != nil || byName(items)[ItemMultipleCompare].Detail != DetailPUndefined {
		t.Errorf("no t: %+v", none)
	}
}

func decodeRequest(t *testing.T, body string) error {
	t.Helper()
	var r Request
	return json.Unmarshal([]byte(body), &r)
}

// Which kinds and combinations are unsupported (unsupported_candidate_kind) and which are plain bad arguments.
func TestSettingsSetRequestValidation(t *testing.T) {
	unsupportedCases := map[string]string{
		"experiment as current":     `{"candidate":{"kind":"shadow","id":"v"},"current":{"kind":"experiment","id":"1"}}`,
		"setting as current":        `{"candidate":{"kind":"shadow","id":"v"},"current":{"kind":"setting","id":"x"}}`,
		"experiment with a current": `{"candidate":{"kind":"experiment","id":"1"},"current":{"kind":"live","id":"live"}}`,
		"setting off the whitelist": `{"candidate":{"kind":"setting","id":"llm.api_key"}}`,
		"cohort as current":         `{"candidate":{"kind":"shadow","id":"v"},"current":{"kind":"live_cohort","id":"m/v"}}`,
	}
	for name, body := range unsupportedCases {
		err := decodeRequest(t, body)
		if _, ok := IsUnsupported(err); !ok || !errors.Is(err, query.ErrBadCursor) {
			t.Errorf("%s: err = %v, want unsupported (and still a bad argument for a generic classifier)", name, err)
		}
	}
	valid := map[string]string{
		"experiment": `{"candidate":{"kind":"experiment","id":"12"}}`,
		"setting":    `{"candidate":{"kind":"setting","id":"risk.paper.max_trade_loss_pct"},"family_size_extra":2,"min_days":12}`,
	}
	for name, body := range valid {
		if err := decodeRequest(t, body); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	invalid := map[string]string{
		"leading zero":     `{"candidate":{"kind":"experiment","id":"012"}}`,
		"zero":             `{"candidate":{"kind":"experiment","id":"0"}}`,
		"not a number":     `{"candidate":{"kind":"experiment","id":"x"}}`,
		"horizon":          `{"candidate":{"kind":"experiment","id":"1"},"horizon":"30m"}`,
		"usd_jpy":          `{"candidate":{"kind":"experiment","id":"1"},"usd_jpy":150,"notional_jpy":1000}`,
		"loose criterion":  `{"candidate":{"kind":"experiment","id":"1"},"min_days":9}`,
		"unknown argument": `{"candidate":{"kind":"experiment","id":"1"},"x":1}`,
	}
	for name, body := range invalid {
		err := decodeRequest(t, body)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if _, ok := IsUnsupported(err); ok {
			t.Errorf("%s: %v is unsupported, want a plain bad argument", name, err)
		}
	}
}
