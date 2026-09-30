package organisms

import (
	"math"
	"testing"
)

// formatFloat must match JavaScript's Number.prototype.toFixed (used by the
// hydrated Lit table): exact decimal ties round away from zero, -0 has no
// sign. fmt's %f would give "12" / "-0.00" for the first and last cases.
func TestFormatFloat_MatchesToFixed(t *testing.T) {
	negZero := math.Copysign(0, -1)
	tests := []struct {
		name     string
		v        float64
		decimals int
		want     string
	}{
		{"tie rounds up, not to even", 12.5, 0, "13"},
		{"tie already odd stays away from zero", 3.5, 0, "4"},
		{"negative tie rounds away from zero", -2.5, 0, "-3"},
		{"one-decimal tie", 100.25, 1, "100.3"},
		{"two-decimal tie", 0.125, 2, "0.13"},
		{"negative two-decimal tie", -0.375, 2, "-0.38"},
		{"not a tie (binary value below .005)", 1.005, 2, "1.00"},
		{"plain value unchanged", 2831.5, 1, "2831.5"},
		{"negative zero has no sign", negZero, 2, "0.00"},
		{"tiny negative keeps its sign like toFixed", -0.004, 2, "-0.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFloat(tt.v, tt.decimals); got != tt.want {
				t.Errorf("formatFloat(%v, %d) = %q, want %q", tt.v, tt.decimals, got, tt.want)
			}
		})
	}
}

func TestFormatSignedFloat_ZeroAndNegativeZeroCarryNoSign(t *testing.T) {
	for _, v := range []float64{0, math.Copysign(0, -1)} {
		if got := formatSignedFloat(v, 2); got != "0.00" {
			t.Errorf("formatSignedFloat(%v, 2) = %q, want %q", v, got, "0.00")
		}
	}
	if got := formatSignedFloat(12.5, 0); got != "+13" {
		t.Errorf("formatSignedFloat(12.5, 0) = %q, want %q", got, "+13")
	}
}
