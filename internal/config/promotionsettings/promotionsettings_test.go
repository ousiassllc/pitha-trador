package promotionsettings_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
)

type fakeStore map[string]string

func (f fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f[key]
	return v, ok, nil
}

// A change that makes a criterion looser than its documented floor is refused, for every key.
func TestLooserThanFloorIsRefused(t *testing.T) {
	for key, v := range map[string]string{
		promotionsettings.KeyMinCommonRows:     "299",
		promotionsettings.KeyMinDays:           "9",
		promotionsettings.KeyMinDiffCILowerBps: "-0.1",
		promotionsettings.KeyMinPositiveDayPct: "49.9",
		promotionsettings.KeyMaxAdjustedP:      "0.06",
	} {
		if promotionsettings.Validate(key, v) == nil {
			t.Errorf("Validate(%s, %s) accepted a looser value", key, v)
		}
	}
}

// Stricter values (and the floor itself) are accepted; an out-of-range or non-numeric one is not.
func TestStricterIsAccepted(t *testing.T) {
	for key, v := range map[string]string{
		promotionsettings.KeyMinCommonRows:     "300",
		promotionsettings.KeyMinDays:           "30",
		promotionsettings.KeyMinDiffCILowerBps: "2.5",
		promotionsettings.KeyMinPositiveDayPct: "60",
		promotionsettings.KeyMaxAdjustedP:      "0.01",
	} {
		if err := promotionsettings.Validate(key, v); err != nil {
			t.Errorf("Validate(%s, %s) = %v", key, v, err)
		}
	}
	for _, v := range []string{`"x"`, "null", "1.5"} {
		if promotionsettings.Validate(promotionsettings.KeyMinDays, v) == nil {
			t.Errorf("Validate(min_days, %s) accepted a bad value", v)
		}
	}
	if promotionsettings.Validate("research.promotion.nope", "1") == nil {
		t.Error("an unknown key was accepted")
	}
}

// Effective applies stricter stored values and ignores a looser one (the floor stays).
func TestEffective(t *testing.T) {
	c, err := promotionsettings.Effective(t.Context(), fakeStore{
		promotionsettings.KeyMinDays:           "20",
		promotionsettings.KeyMinCommonRows:     "10", // looser: ignored
		promotionsettings.KeyMaxAdjustedP:      "0.01",
		promotionsettings.KeyMinDiffCILowerBps: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := promotionsettings.Floor()
	want.MinDays, want.MaxAdjustedP, want.MinDiffCILowerBps = 20, 0.01, 1
	if c != want {
		t.Errorf("Effective = %+v, want %+v", c, want)
	}
	if got, _ := promotionsettings.Effective(t.Context(), fakeStore{}); got != promotionsettings.Floor() {
		t.Errorf("nothing stored: %+v, want the floor", got)
	}
}

// Per-call overrides follow the same rule: stricter than what is in force only.
func TestTightenRefusesLoosening(t *testing.T) {
	c := promotionsettings.Floor()
	if err := c.Tighten(promotionsettings.KeyMinDays, 25); err != nil || c.MinDays != 25 {
		t.Fatalf("tighten to 25: %v, %+v", err, c)
	}
	if err := c.Tighten(promotionsettings.KeyMinDays, 15); err == nil || c.MinDays != 25 {
		t.Errorf("loosening 25 -> 15 must fail and keep 25: %v, %+v", err, c)
	}
	if err := c.Tighten(promotionsettings.KeyMaxAdjustedP, 0.5); err == nil || c.MaxAdjustedP != 0.05 {
		t.Errorf("a larger max p must fail: %v, %+v", err, c)
	}
}
