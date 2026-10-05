package atoms_test

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
)

func TestFormatJST(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"TSE open 00:30Z is 09:30 JST", time.Date(2026, 10, 5, 0, 30, 0, 0, time.UTC), "2026-10-05 09:30:00 JST"},
		{"UTC evening crosses into next JST day", time.Date(2026, 10, 5, 15, 30, 0, 0, time.UTC), "2026-10-06 00:30:00 JST"},
		{"JST input is unchanged", time.Date(2026, 10, 5, 9, 30, 15, 0, atoms.JST), "2026-10-05 09:30:15 JST"},
		{"fractional seconds are dropped", time.Date(2026, 10, 5, 0, 30, 0, 999_000_000, time.UTC), "2026-10-05 09:30:00 JST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atoms.FormatJST(tt.in); got != tt.want {
				t.Errorf("FormatJST(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Same instant in other zones (and the host-local zone) must render the
// same string: nothing may depend on time.Local or the input's location.
func TestFormatJST_IndependentOfInputZoneAndHostLocal(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	instant := time.Date(2026, 10, 5, 0, 30, 0, 0, time.UTC)
	prev := time.Local
	t.Cleanup(func() { time.Local = prev })
	time.Local = ny

	for name, in := range map[string]time.Time{"utc": instant, "new york": instant.In(ny), "local": instant.Local()} {
		if got, want := atoms.FormatJST(in), "2026-10-05 09:30:00 JST"; got != want {
			t.Errorf("%s: FormatJST = %q, want %q", name, got, want)
		}
	}
}
