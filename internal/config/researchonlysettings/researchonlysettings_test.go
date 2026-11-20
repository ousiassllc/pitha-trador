package researchonlysettings

import (
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
)

func TestKeysAreJobKeysThenPromotionKeys(t *testing.T) {
	want := append(slices.Clone(researchjobsettings.Keys()), promotionsettings.Keys()...)
	got := Keys()
	if !slices.Equal(got, want) {
		t.Fatalf("Keys() = %v, want %v", got, want)
	}
	if len(got) != len(researchjobsettings.Keys())+len(promotionsettings.Keys()) {
		t.Fatalf("Keys() has %d entries", len(got))
	}
	seen := map[string]bool{}
	for _, k := range got {
		if seen[k] {
			t.Fatalf("duplicate key %q", k)
		}
		seen[k] = true
	}
}

func TestKeysDoesNotAliasThePackageSlices(t *testing.T) {
	first := Keys()
	first[0] = "tampered"
	if Keys()[0] != researchjobsettings.Keys()[0] {
		t.Fatalf("mutating the result of Keys() changed a later call: %q", Keys()[0])
	}
}

func TestIsKeyIsTrueOnlyForBothFamilies(t *testing.T) {
	for _, k := range researchjobsettings.Keys() {
		if !IsKey(k) {
			t.Errorf("IsKey(%q) = false, want true (researchjobsettings)", k)
		}
	}
	for _, k := range promotionsettings.Keys() {
		if !IsKey(k) {
			t.Errorf("IsKey(%q) = false, want true (promotionsettings)", k)
		}
	}
	for _, k := range []string{"", "research", "research.eval", "research.eval.enabled ", "RESEARCH.EVAL.ENABLED", "research.promotion.unknown", "research.tachibana_bars.enabled", "jev.enabled"} {
		if IsKey(k) {
			t.Errorf("IsKey(%q) = true, want false", k)
		}
	}
}

func TestIsIntegerKeyIsSwitchesRetentionsAndWholeNumberCriteria(t *testing.T) {
	integer := map[string]bool{
		researchjobsettings.KeyEvalEnabled:        true,
		researchjobsettings.KeyEvalRetentionDays:  true,
		researchjobsettings.KeyModelEnabled:       true,
		researchjobsettings.KeyModelRetentionDays: true,
		researchjobsettings.KeyExportEnabled:      true,
		promotionsettings.KeyMinCommonRows:        true,
		promotionsettings.KeyMinDays:              true,
		promotionsettings.KeyMinDiffCILowerBps:    false,
		promotionsettings.KeyMinPositiveDayPct:    false,
		promotionsettings.KeyMaxAdjustedP:         false,
	}
	if len(integer) != len(Keys()) {
		t.Fatalf("the table covers %d keys, Keys() has %d", len(integer), len(Keys()))
	}
	for _, k := range Keys() {
		want, ok := integer[k]
		if !ok {
			t.Errorf("key %q is missing from the table", k)
			continue
		}
		if got := IsIntegerKey(k); got != want {
			t.Errorf("IsIntegerKey(%q) = %v, want %v", k, got, want)
		}
	}
	for _, k := range []string{"", "research.unknown", "research.promotion.unknown"} {
		if IsIntegerKey(k) {
			t.Errorf("IsIntegerKey(%q) = true for a key outside both families", k)
		}
	}
}

func TestDefaultTextDelegatesByFamily(t *testing.T) {
	want := map[string]string{
		// researchjobsettings: the switches are off, the retentions are their defaults.
		researchjobsettings.KeyEvalEnabled:        "0",
		researchjobsettings.KeyEvalRetentionDays:  "365",
		researchjobsettings.KeyModelEnabled:       "0",
		researchjobsettings.KeyModelRetentionDays: "90",
		researchjobsettings.KeyExportEnabled:      "0",
		// promotionsettings: the documented floors.
		promotionsettings.KeyMinCommonRows:     "300",
		promotionsettings.KeyMinDays:           "10",
		promotionsettings.KeyMinDiffCILowerBps: "0",
		promotionsettings.KeyMinPositiveDayPct: "50",
		promotionsettings.KeyMaxAdjustedP:      "0.05",
	}
	for _, k := range Keys() {
		w, ok := want[k]
		if !ok {
			t.Errorf("key %q is missing from the table", k)
			continue
		}
		if got := DefaultText(k); got != w {
			t.Errorf("DefaultText(%q) = %q, want %q", k, got, w)
		}
	}
	for _, k := range researchjobsettings.Keys() {
		if got, w := DefaultText(k), researchjobsettings.DefaultText(k); got != w {
			t.Errorf("DefaultText(%q) = %q, researchjobsettings says %q", k, got, w)
		}
	}
	for _, k := range promotionsettings.Keys() {
		if got, w := DefaultText(k), promotionsettings.DefaultText(k); got != w {
			t.Errorf("DefaultText(%q) = %q, promotionsettings says %q", k, got, w)
		}
	}
}

func TestEverySwitchDefaultsToOff(t *testing.T) {
	for _, k := range []string{researchjobsettings.KeyEvalEnabled, researchjobsettings.KeyModelEnabled, researchjobsettings.KeyExportEnabled} {
		if got := DefaultText(k); got != "0" {
			t.Errorf("DefaultText(%q) = %q, want 0 (off)", k, got)
		}
	}
}

func TestEveryDefaultTextValidates(t *testing.T) {
	for _, k := range Keys() {
		if err := Validate(k, DefaultText(k)); err != nil {
			t.Errorf("Validate(%q, default %q): %v", k, DefaultText(k), err)
		}
	}
}

func TestValidateAcceptsValuesOfTheJobFamily(t *testing.T) {
	cases := []struct{ key, value string }{
		{researchjobsettings.KeyEvalEnabled, "0"},
		{researchjobsettings.KeyEvalEnabled, "1"},
		{researchjobsettings.KeyModelEnabled, "0"},
		{researchjobsettings.KeyExportEnabled, "1"},
		{researchjobsettings.KeyEvalRetentionDays, "1"},
		{researchjobsettings.KeyModelRetentionDays, "3650"},
	}
	for _, c := range cases {
		if err := Validate(c.key, c.value); err != nil {
			t.Errorf("Validate(%q, %q) = %v, want nil", c.key, c.value, err)
		}
	}
}

func TestValidateRejectsBrokenValuesOfTheJobFamilyLikeItsPackage(t *testing.T) {
	cases := []struct{ key, value string }{
		{researchjobsettings.KeyEvalEnabled, "2"},
		{researchjobsettings.KeyEvalEnabled, "-1"},
		{researchjobsettings.KeyModelEnabled, "0.5"},
		{researchjobsettings.KeyExportEnabled, "true"},
		{researchjobsettings.KeyExportEnabled, "null"},
		{researchjobsettings.KeyEvalRetentionDays, "0"},
		{researchjobsettings.KeyModelRetentionDays, "3651"},
		{researchjobsettings.KeyModelRetentionDays, "90.5"},
		{researchjobsettings.KeyEvalRetentionDays, `"365"`},
		{researchjobsettings.KeyEvalRetentionDays, ""},
	}
	for _, c := range cases {
		got := Validate(c.key, c.value)
		if got == nil {
			t.Errorf("Validate(%q, %q) = nil, want an error", c.key, c.value)
			continue
		}
		if want := researchjobsettings.Validate(c.key, c.value); want == nil || got.Error() != want.Error() {
			t.Errorf("Validate(%q, %q) = %v, researchjobsettings says %v", c.key, c.value, got, want)
		}
	}
}

func TestValidateAcceptsCriteriaAtOrStricterThanTheFloor(t *testing.T) {
	cases := []struct{ key, value string }{
		{promotionsettings.KeyMinCommonRows, "300"},
		{promotionsettings.KeyMinCommonRows, "1000"},
		{promotionsettings.KeyMinDays, "10"},
		{promotionsettings.KeyMinDays, "30"},
		{promotionsettings.KeyMinDiffCILowerBps, "0"},
		{promotionsettings.KeyMinDiffCILowerBps, "1.5"},
		{promotionsettings.KeyMinPositiveDayPct, "50"},
		{promotionsettings.KeyMinPositiveDayPct, "60.5"},
		{promotionsettings.KeyMaxAdjustedP, "0.05"},
		{promotionsettings.KeyMaxAdjustedP, "0.01"},
	}
	for _, c := range cases {
		if err := Validate(c.key, c.value); err != nil {
			t.Errorf("Validate(%q, %q) = %v, want nil", c.key, c.value, err)
		}
	}
}

func TestValidateRejectsLooseOrBrokenPromotionCriteria(t *testing.T) {
	cases := []struct{ name, key, value string }{
		{"min_common_rows looser than the floor", promotionsettings.KeyMinCommonRows, "299"},
		{"min_days looser than the floor", promotionsettings.KeyMinDays, "9"},
		{"min_diff_ci_lower_bps below zero", promotionsettings.KeyMinDiffCILowerBps, "-0.1"},
		{"min_positive_day_pct looser than the floor", promotionsettings.KeyMinPositiveDayPct, "49.9"},
		{"max_adjusted_p looser than the floor", promotionsettings.KeyMaxAdjustedP, "0.06"},
		{"max_adjusted_p of 1", promotionsettings.KeyMaxAdjustedP, "1"},
		{"min_common_rows above the hard bound", promotionsettings.KeyMinCommonRows, "10000001"},
		{"min_positive_day_pct above 100", promotionsettings.KeyMinPositiveDayPct, "100.1"},
		{"a fraction for a whole-number criterion", promotionsettings.KeyMinDays, "10.5"},
		{"not a number", promotionsettings.KeyMinCommonRows, `"300"`},
		{"null", promotionsettings.KeyMaxAdjustedP, "null"},
		{"empty", promotionsettings.KeyMinDays, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Validate(c.key, c.value)
			if got == nil {
				t.Fatalf("Validate(%q, %q) = nil, want an error", c.key, c.value)
			}
			if want := promotionsettings.Validate(c.key, c.value); want == nil || got.Error() != want.Error() {
				t.Fatalf("Validate(%q, %q) = %v, promotionsettings says %v", c.key, c.value, got, want)
			}
		})
	}
}

func TestValidateIsNotCrossedBetweenFamilies(t *testing.T) {
	// 0 is a valid switch but a criterion looser than the floor; 300 is a valid criterion but not a switch.
	if err := Validate(promotionsettings.KeyMinDays, "0"); err == nil {
		t.Error("a promotion criterion of 0 must be refused: it is looser than the floor")
	}
	if err := Validate(researchjobsettings.KeyEvalEnabled, "300"); err == nil {
		t.Error("a switch of 300 must be refused")
	}
}
