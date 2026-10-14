package dailyguardsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

type mapStore struct {
	rows map[string]string
	err  error
}

func (m mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m.rows[key]
	return v, ok, m.err
}

func TestLoad_UnsetDisablesEveryRule(t *testing.T) {
	l, err := dailyguardsettings.Load(context.Background(), mapStore{})
	if err != nil || l.AnyEnabled() {
		t.Fatalf("Load = %+v, %v; want everything disabled", l, err)
	}
}

func TestLoad_ReadsEachThreshold(t *testing.T) {
	l, err := dailyguardsettings.Load(context.Background(), mapStore{rows: map[string]string{
		dailyguardsettings.KeyLossLimitJPY:          "50000",
		dailyguardsettings.KeyProfitLockTriggerJPY:  "20000",
		dailyguardsettings.KeyProfitLockGivebackPct: "30",
		dailyguardsettings.KeyMaxTrades:             "7",
	}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := dailyguardsettings.Limits{LossLimitJPY: 50000, ProfitLockTriggerJPY: 20000, ProfitLockGivebackPct: 30, MaxTrades: 7}
	if l != want {
		t.Fatalf("Load = %+v, want %+v", l, want)
	}
	if !l.LossLimitEnabled() || !l.ProfitLockEnabled() || !l.MaxTradesEnabled() {
		t.Fatalf("rules not all enabled: %+v", l)
	}
}

func TestProfitLockNeedsBothThresholds(t *testing.T) {
	if (dailyguardsettings.Limits{ProfitLockTriggerJPY: 1}).ProfitLockEnabled() || (dailyguardsettings.Limits{ProfitLockGivebackPct: 1}).ProfitLockEnabled() {
		t.Fatal("profit lock enabled with only one of its thresholds")
	}
}

func TestLoad_DamagedRowDisablesOnlyThatRule(t *testing.T) {
	l, err := dailyguardsettings.Load(context.Background(), mapStore{rows: map[string]string{
		dailyguardsettings.KeyLossLimitJPY: "not json",
		dailyguardsettings.KeyMaxTrades:    "3",
	}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if l.LossLimitEnabled() || l.MaxTrades != 3 {
		t.Fatalf("Load = %+v, want loss limit off and max trades 3", l)
	}
}

func TestLoad_ReadFailureIsAnError(t *testing.T) {
	if _, err := dailyguardsettings.Load(context.Background(), mapStore{err: errors.New("db down")}); err == nil {
		t.Fatal("Load swallowed a store read failure")
	}
}

func TestApply_Ranges(t *testing.T) {
	for name, tc := range map[string]struct {
		key, raw string
		ok       bool
	}{
		"zero disables":           {dailyguardsettings.KeyLossLimitJPY, "0", true},
		"negative":                {dailyguardsettings.KeyLossLimitJPY, "-0.5", false},
		"giveback 100":            {dailyguardsettings.KeyProfitLockGivebackPct, "100", true},
		"giveback just above 100": {dailyguardsettings.KeyProfitLockGivebackPct, "100.01", false},
		"integer trades":          {dailyguardsettings.KeyMaxTrades, "10", true},
		"fractional trades":       {dailyguardsettings.KeyMaxTrades, "1.5", false},
		"string":                  {dailyguardsettings.KeyMaxTrades, `"5"`, false},
		"unknown key":             {"risk.nope", "1", false},
	} {
		var l dailyguardsettings.Limits
		if err := dailyguardsettings.Apply(&l, tc.key, tc.raw); (err == nil) != tc.ok {
			t.Errorf("%s: Apply(%s, %s) error = %v, want ok=%v", name, tc.key, tc.raw, err, tc.ok)
		}
	}
}
