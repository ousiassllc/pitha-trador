package dailyguardsettings_test

import (
	"context"
	"errors"
	"slices"
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
		"zero disables":                {dailyguardsettings.KeyLossLimitJPY, "0", true},
		"negative":                     {dailyguardsettings.KeyLossLimitJPY, "-0.5", false},
		"giveback 100":                 {dailyguardsettings.KeyProfitLockGivebackPct, "100", true},
		"giveback just above 100":      {dailyguardsettings.KeyProfitLockGivebackPct, "100.01", false},
		"integer trades":               {dailyguardsettings.KeyMaxTrades, "10", true},
		"fractional trades":            {dailyguardsettings.KeyMaxTrades, "1.5", false},
		"string":                       {dailyguardsettings.KeyMaxTrades, `"5"`, false},
		"unknown key":                  {"risk.nope", "1", false},
		"capital":                      {dailyguardsettings.KeyOperatingCapitalJPY, "500000", true},
		"capital zero":                 {dailyguardsettings.KeyOperatingCapitalJPY, "0", true},
		"capital negative":             {dailyguardsettings.KeyOperatingCapitalJPY, "-1", false},
		"capital above 100 is fine":    {dailyguardsettings.KeyOperatingCapitalJPY, "1000000", true},
		"loss pct":                     {dailyguardsettings.KeyLossLimitPct, "1.5", true},
		"loss pct 100":                 {dailyguardsettings.KeyLossLimitPct, "100", true},
		"loss pct above 100":           {dailyguardsettings.KeyLossLimitPct, "100.1", false},
		"loss pct negative":            {dailyguardsettings.KeyLossLimitPct, "-0.1", false},
		"trigger pct":                  {dailyguardsettings.KeyProfitLockTriggerPct, "0.5", true},
		"trigger pct above 100":        {dailyguardsettings.KeyProfitLockTriggerPct, "101", false},
		"trigger pct negative":         {dailyguardsettings.KeyProfitLockTriggerPct, "-1", false},
		"loss pct not a number":        {dailyguardsettings.KeyLossLimitPct, `"1"`, false},
		"direct loss yen above 100":    {dailyguardsettings.KeyLossLimitJPY, "50000", true},
		"direct trigger yen above 100": {dailyguardsettings.KeyProfitLockTriggerJPY, "20000", true},
	} {
		var s dailyguardsettings.Settings
		if err := dailyguardsettings.Apply(&s, tc.key, tc.raw); (err == nil) != tc.ok {
			t.Errorf("%s: Apply(%s, %s) error = %v, want ok=%v", name, tc.key, tc.raw, err, tc.ok)
		}
	}
}

func TestApply_RejectedValueLeavesSettingsUntouched(t *testing.T) {
	var s dailyguardsettings.Settings
	if err := dailyguardsettings.Apply(&s, dailyguardsettings.KeyLossLimitPct, "101"); err == nil {
		t.Fatal("Apply accepted 101%")
	}
	if s != (dailyguardsettings.Settings{}) {
		t.Fatalf("Settings = %+v, want untouched", s)
	}
}

func TestKeys_ContainsEveryKeyOnceAndIsKeyAgrees(t *testing.T) {
	want := []string{
		dailyguardsettings.KeyOperatingCapitalJPY, dailyguardsettings.KeyLossLimitPct, dailyguardsettings.KeyProfitLockTriggerPct,
		dailyguardsettings.KeyProfitLockGivebackPct, dailyguardsettings.KeyMaxTrades,
		dailyguardsettings.KeyLossLimitJPY, dailyguardsettings.KeyProfitLockTriggerJPY,
	}
	got := dailyguardsettings.Keys()
	if len(got) != len(want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
	for _, key := range want {
		if !slices.Contains(got, key) || !dailyguardsettings.IsKey(key) {
			t.Errorf("key %q missing from Keys()/IsKey", key)
		}
	}
	if dailyguardsettings.IsKey(dailyguardsettings.SeededMarkerKey) {
		t.Error("the seed marker must not be a Settings key")
	}
	if !dailyguardsettings.IsIntegerKey(dailyguardsettings.KeyMaxTrades) || dailyguardsettings.IsIntegerKey(dailyguardsettings.KeyLossLimitPct) {
		t.Error("MaxTrades must be the only integer key")
	}
}
