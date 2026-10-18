package dailyguardsettings_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

func loadResolved(t *testing.T, rows map[string]string) dailyguardsettings.Resolved {
	t.Helper()
	r, err := dailyguardsettings.LoadResolved(context.Background(), mapStore{rows: rows})
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	return r
}

// A change of the operating capital changes the derived yen values.
func TestResolve_CapitalChangeChangesDerivedYen(t *testing.T) {
	rows := map[string]string{
		dailyguardsettings.KeyOperatingCapitalJPY:   "500000",
		dailyguardsettings.KeyLossLimitPct:          "1",
		dailyguardsettings.KeyProfitLockTriggerPct:  "0.5",
		dailyguardsettings.KeyProfitLockGivebackPct: "50",
		dailyguardsettings.KeyMaxTrades:             "20",
	}
	r := loadResolved(t, rows)
	want := dailyguardsettings.Limits{LossLimitJPY: 5000, ProfitLockTriggerJPY: 2500, ProfitLockGivebackPct: 50, MaxTrades: 20}
	if r.Limits != want {
		t.Fatalf("Limits = %+v, want %+v", r.Limits, want)
	}
	if r.CapitalJPY != 500000 || r.LossLimit != (dailyguardsettings.Rule{Source: dailyguardsettings.SourcePercent, Pct: 1, JPY: 5000}) ||
		r.ProfitLockTrigger != (dailyguardsettings.Rule{Source: dailyguardsettings.SourcePercent, Pct: 0.5, JPY: 2500}) {
		t.Fatalf("derivation info = %+v", r)
	}

	rows[dailyguardsettings.KeyOperatingCapitalJPY] = "1000000"
	r = loadResolved(t, rows)
	if r.Limits.LossLimitJPY != 10000 || r.Limits.ProfitLockTriggerJPY != 5000 {
		t.Fatalf("after doubling the capital Limits = %+v, want 10000 / 5000", r.Limits)
	}
}

func TestResolve_RoundsToWholeYen(t *testing.T) {
	r := dailyguardsettings.Resolve(settings(map[string]string{
		dailyguardsettings.KeyOperatingCapitalJPY:  "123456",
		dailyguardsettings.KeyLossLimitPct:         "1",    // 1234.56 -> 1235
		dailyguardsettings.KeyProfitLockTriggerPct: "0.33", // 407.4048 -> 407
	}))
	if r.Limits.LossLimitJPY != 1235 || r.Limits.ProfitLockTriggerJPY != 407 {
		t.Fatalf("Limits = %+v, want 1235 / 407", r.Limits)
	}
}

func TestResolve_PercentWinsOverDirectYen(t *testing.T) {
	r := loadResolved(t, map[string]string{
		dailyguardsettings.KeyOperatingCapitalJPY:  "500000",
		dailyguardsettings.KeyLossLimitPct:         "1",
		dailyguardsettings.KeyLossLimitJPY:         "50000",
		dailyguardsettings.KeyProfitLockTriggerPct: "0.5",
		dailyguardsettings.KeyProfitLockTriggerJPY: "20000",
	})
	if r.Limits.LossLimitJPY != 5000 || r.Limits.ProfitLockTriggerJPY != 2500 {
		t.Fatalf("Limits = %+v, want the percent-derived 5000 / 2500", r.Limits)
	}
	if r.LossLimit.Source != dailyguardsettings.SourcePercent || r.ProfitLockTrigger.Source != dailyguardsettings.SourcePercent {
		t.Fatalf("sources = %v / %v, want percent", r.LossLimit.Source, r.ProfitLockTrigger.Source)
	}
}

// Rows written before #782 (direct yen only, no capital) behave exactly as before.
func TestResolve_DirectYenOnlyKeepsLegacyLimits(t *testing.T) {
	legacy := map[string]string{
		dailyguardsettings.KeyLossLimitJPY:          "50000",
		dailyguardsettings.KeyProfitLockTriggerJPY:  "20000",
		dailyguardsettings.KeyProfitLockGivebackPct: "30",
		dailyguardsettings.KeyMaxTrades:             "7",
	}
	r := loadResolved(t, legacy)
	want := dailyguardsettings.Limits{LossLimitJPY: 50000, ProfitLockTriggerJPY: 20000, ProfitLockGivebackPct: 30, MaxTrades: 7}
	if r.Limits != want {
		t.Fatalf("Limits = %+v, want %+v", r.Limits, want)
	}
	if r.LossLimit.Source != dailyguardsettings.SourceDirect || r.ProfitLockTrigger.Source != dailyguardsettings.SourceDirect {
		t.Fatalf("sources = %v / %v, want direct", r.LossLimit.Source, r.ProfitLockTrigger.Source)
	}

	// A capital alone does not touch direct-yen rules.
	legacy[dailyguardsettings.KeyOperatingCapitalJPY] = "500000"
	if r := loadResolved(t, legacy); r.Limits != want {
		t.Fatalf("with only a capital added Limits = %+v, want %+v", r.Limits, want)
	}
}

func TestResolve_PercentWithoutCapitalIsDisabled(t *testing.T) {
	r := loadResolved(t, map[string]string{
		dailyguardsettings.KeyLossLimitPct:          "1",
		dailyguardsettings.KeyProfitLockTriggerPct:  "0.5",
		dailyguardsettings.KeyProfitLockGivebackPct: "50",
	})
	if r.Limits.LossLimitEnabled() || r.Limits.ProfitLockEnabled() {
		t.Fatalf("Limits = %+v, want loss limit and profit lock off", r.Limits)
	}
	if !r.LossLimit.NeedsCapital() || !r.ProfitLockTrigger.NeedsCapital() || r.LossLimit.Pct != 1 {
		t.Fatalf("rules = %+v / %+v, want NeedsCapital with Pct kept", r.LossLimit, r.ProfitLockTrigger)
	}

	// An explicit capital of 0 is the same as unset.
	r = loadResolved(t, map[string]string{dailyguardsettings.KeyOperatingCapitalJPY: "0", dailyguardsettings.KeyLossLimitPct: "1"})
	if r.Limits.LossLimitEnabled() || !r.LossLimit.NeedsCapital() {
		t.Fatalf("capital 0: %+v", r)
	}
}

// A percent rule with no capital falls back to a stored direct yen value.
func TestResolve_PercentWithoutCapitalFallsBackToDirectYen(t *testing.T) {
	r := loadResolved(t, map[string]string{
		dailyguardsettings.KeyLossLimitPct: "1",
		dailyguardsettings.KeyLossLimitJPY: "50000",
	})
	if r.Limits.LossLimitJPY != 50000 || r.LossLimit.Source != dailyguardsettings.SourceDirect || r.LossLimit.NeedsCapital() {
		t.Fatalf("Resolved = %+v, want the direct 50000", r)
	}
}

func TestResolve_CapitalWithoutPercentIsDisabled(t *testing.T) {
	r := loadResolved(t, map[string]string{dailyguardsettings.KeyOperatingCapitalJPY: "500000"})
	if r.Limits.AnyEnabled() || r.CapitalJPY != 500000 {
		t.Fatalf("Resolved = %+v, want capital shown and every rule off", r)
	}
	if r.LossLimit.Source != dailyguardsettings.SourceNone || r.LossLimit.NeedsCapital() {
		t.Fatalf("LossLimit = %+v, want plain none", r.LossLimit)
	}
}

func TestLoadResolved_NothingStoredDisablesEverything(t *testing.T) {
	r := loadResolved(t, nil)
	if r.Limits.AnyEnabled() || r != (dailyguardsettings.Resolved{}) {
		t.Fatalf("Resolved = %+v, want the zero value", r)
	}
}

func TestLoadResolved_DamagedRowIsIgnored(t *testing.T) {
	r := loadResolved(t, map[string]string{
		dailyguardsettings.KeyOperatingCapitalJPY: "not json",
		dailyguardsettings.KeyLossLimitPct:        "1",
		dailyguardsettings.KeyLossLimitJPY:        "40000",
		dailyguardsettings.KeyMaxTrades:           "3",
	})
	if r.CapitalJPY != 0 || r.Limits.LossLimitJPY != 40000 || r.Limits.MaxTrades != 3 {
		t.Fatalf("Resolved = %+v, want capital ignored so the direct 40000 applies", r)
	}
}

func settings(rows map[string]string) dailyguardsettings.Settings {
	var s dailyguardsettings.Settings
	for key, raw := range rows {
		if err := dailyguardsettings.Apply(&s, key, raw); err != nil {
			panic(err)
		}
	}
	return s
}

func ptr(v float64) *float64 { return &v }

func TestResolve_Boundaries(t *testing.T) {
	for name, tc := range map[string]struct {
		capital, pct float64
		wantJPY      float64
		wantSource   dailyguardsettings.Source
	}{
		"capital 0":                   {0, 1, 0, dailyguardsettings.SourceNone},
		"pct 0":                       {500000, 0, 0, dailyguardsettings.SourceNone},
		"pct 100":                     {500000, 100, 500000, dailyguardsettings.SourcePercent},
		"capital at the cap, pct 100": {dailyguardsettings.MaxCapitalJPY, 100, dailyguardsettings.MaxCapitalJPY, dailyguardsettings.SourcePercent},
	} {
		r := dailyguardsettings.Resolve(dailyguardsettings.Settings{CapitalJPY: ptr(tc.capital), LossLimitPct: ptr(tc.pct)})
		if r.Limits.LossLimitJPY != tc.wantJPY || r.LossLimit.Source != tc.wantSource {
			t.Errorf("%s: LossLimit = %+v (Limits %+v), want %v yen from source %v", name, r.LossLimit, r.Limits, tc.wantJPY, tc.wantSource)
		}
	}
}

func TestApply_RejectsCapitalAboveCap(t *testing.T) {
	var s dailyguardsettings.Settings
	if err := dailyguardsettings.Apply(&s, dailyguardsettings.KeyOperatingCapitalJPY, "1e12"); err != nil || s.CapitalJPY == nil {
		t.Fatalf("capital at the cap: err=%v settings=%+v, want accepted", err, s)
	}
	var over dailyguardsettings.Settings
	for _, raw := range []string{"1000000000001", "1e308"} {
		err := dailyguardsettings.Apply(&over, dailyguardsettings.KeyOperatingCapitalJPY, raw)
		if err == nil || !strings.Contains(err.Error(), "1 兆円") {
			t.Errorf("capital %s: err = %v, want the 1 兆円 limit message", raw, err)
		}
	}
	if over.CapitalJPY != nil {
		t.Fatalf("rejected capital was stored: %+v", over)
	}
	// The cap is the capital's own; a yen threshold may exceed it.
	if err := dailyguardsettings.Apply(&over, dailyguardsettings.KeyLossLimitJPY, "2e12"); err != nil {
		t.Fatalf("direct yen above the capital cap: %v", err)
	}
}

// A non-finite result never reaches the risk engine: the rule is off.
func TestResolve_NonFiniteResultDisablesTheRule(t *testing.T) {
	for name, s := range map[string]dailyguardsettings.Settings{
		"infinite capital": {CapitalJPY: ptr(math.Inf(1)), LossLimitPct: ptr(1)},
		"NaN capital":      {CapitalJPY: ptr(math.NaN()), LossLimitPct: ptr(1)},
		"infinite pct":     {CapitalJPY: ptr(500000), LossLimitPct: ptr(math.Inf(1))},
	} {
		r := dailyguardsettings.Resolve(s)
		if r.Limits.LossLimitJPY != 0 || r.Limits.LossLimitEnabled() || r.LossLimit.Source != dailyguardsettings.SourceNone {
			t.Errorf("%s: Resolved = %+v, want the loss limit off", name, r)
		}
	}
}

// Percent wins even when it rounds to 0 yen: the rule is off and the direct
// yen row is not used as a fallback.
func TestResolve_PercentRoundingToZeroYenDisablesDespiteDirectYen(t *testing.T) {
	r := loadResolved(t, map[string]string{
		dailyguardsettings.KeyOperatingCapitalJPY: "10",
		dailyguardsettings.KeyLossLimitPct:        "1", // 0.1 yen -> 0
		dailyguardsettings.KeyLossLimitJPY:        "50000",
	})
	if r.Limits.LossLimitEnabled() || r.Limits.LossLimitJPY != 0 {
		t.Fatalf("Limits = %+v, want the loss limit off", r.Limits)
	}
	if r.LossLimit.Source != dailyguardsettings.SourcePercent || r.LossLimit.JPY != 0 {
		t.Fatalf("LossLimit = %+v, want percent source with 0 yen", r.LossLimit)
	}
}
