package observation_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// issue #1055: nothing on the trading side reads the watch / observation
// settings or the observation gate. The Policy Engine, the Risk Engine, order
// execution (Paper and Live), the meta-model and the Jev Trader and its shadow
// stay free of both packages, so an observation symbol has no path to an entry
// or an order, and the day's observation spend cannot throttle them.
func TestTradingPackagesDoNotReadTheObservationSettings(t *testing.T) {
	root := filepath.Join("..", "..") // internal/service
	forbidden := []string{"config/watchsettings", "jev/observation"}
	for _, dir := range []string{"policy", "risk", "execution", "metalabel", "backtest", "regime", filepath.Join("jev", "shadow")} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, pkg := range forbidden {
				if strings.Contains(string(src), pkg) {
					t.Errorf("%s refers to %s: the trading path must not read the observation settings", path, pkg)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// jev.Trader (trader.go) is also free of them; only scout.go / observation.go
	// of the jev package know the gate.
	for _, name := range []string{"trader.go", "evaluate.go"} {
		src, err := os.ReadFile(filepath.Join("..", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "ObservationGate") || strings.Contains(string(src), "watchsettings") {
			t.Errorf("jev/%s refers to the observation gate", name)
		}
	}
}

// The self-improvement loop (Sol/Opus) can change policy.* thresholds only; the
// watch size, the observation and its cap are outside every automatic write
// path.
func TestWatchKeysAreOutsideTheSelfImprovementLoop(t *testing.T) {
	for _, key := range append(watchsettings.Keys(), watchsettings.KeyObservationUsage) {
		err := domain.ValidatePolicyChanges([]domain.PolicyChange{{Key: key, OldValue: "45", NewValue: "20"}})
		if err == nil {
			t.Errorf("ValidatePolicyChanges accepts %q: the self-improvement loop could change it", key)
		}
	}
}
