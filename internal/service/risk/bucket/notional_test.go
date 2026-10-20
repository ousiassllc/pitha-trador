package bucket_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/risk/bucket"
)

// Notional is quantity at the latest mark; the entry price only until the first
// mark arrives. The two prices differ so a swapped argument order shows.
func TestNotional_UsesTheMarkAndFallsBackToEntryUntilMarked(t *testing.T) {
	for name, tc := range map[string]struct {
		quantity       int64
		entry, current float64
		want           float64
	}{
		"marked below entry": {10, 2000, 1800, 18_000},
		"marked above entry": {10, 2000, 2300, 23_000},
		"unmarked":           {10, 2000, 0, 20_000},
	} {
		t.Run(name, func(t *testing.T) {
			if got := bucket.Notional(tc.quantity, tc.entry, tc.current); got != tc.want {
				t.Errorf("Notional(%d, %v, %v) = %v, want %v", tc.quantity, tc.entry, tc.current, got, tc.want)
			}
		})
	}
}
