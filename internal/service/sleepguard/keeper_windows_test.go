//go:build windows

package sleepguard_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/sleepguard"
)

// The Windows Keeper: Hold and Release are idempotent and can alternate. This
// runs only on Windows (CI test-windows); whether the OS really stays awake is
// checked by hand with powercfg /requests.
func TestWindowsKeeperHoldAndReleaseAreIdempotent(t *testing.T) {
	k := sleepguard.NewKeeper()
	steps := []struct {
		name string
		fn   func() error
	}{
		{"Hold", k.Hold}, {"Hold again", k.Hold}, {"Release", k.Release}, {"Release again", k.Release},
		{"Hold after Release", k.Hold}, {"Release at the end", k.Release},
	}
	for _, s := range steps {
		if err := s.fn(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
}
