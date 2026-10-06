package atoms_test

import (
	"math"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

func TestFormatConfidence(t *testing.T) {
	ptr := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		in   *float64
		want string
	}{
		{"nil is not evaluated", nil, "—"},
		// Exactly representable halves: %.0f would round to even (12/62/2).
		{"0.125 rounds half away from zero", ptr(0.125), "13%"},
		{"0.625 rounds half away from zero", ptr(0.625), "63%"},
		{"0.025 rounds half away from zero", ptr(0.025), "3%"},
		{"plain value", ptr(0.74), "74%"},
		{"zero", ptr(0), "0%"},
		{"negative zero carries no sign", ptr(math.Copysign(0, -1)), "0%"},
		{"one", ptr(1), "100%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atoms.FormatConfidence(tt.in); got != tt.want {
				t.Errorf("FormatConfidence() = %q, want %q", got, tt.want)
			}
		})
	}
}
