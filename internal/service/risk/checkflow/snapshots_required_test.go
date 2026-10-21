package checkflow_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/risk"
)

// An Engine without a snapshot source cannot price or size an entry, so it
// must not be built at all rather than approve entries with those limits
// skipped (issue #841).
func TestNewEngine_PanicsWithoutSnapshots(t *testing.T) {
	db := newTestDB(t)
	defer func() {
		got := recover()
		msg, _ := got.(string)
		if !strings.Contains(msg, "Snapshots is required") {
			t.Fatalf("NewEngine without Snapshots panicked with %v, want \"cfg.Snapshots is required\"", got)
		}
	}()
	risk.NewEngine(risk.Config{
		Limits:     testLimits(),
		KillSwitch: system.NewKillSwitchRepository(db),
		Settings:   system.NewRuntimeSettingsRepository(db),
	})
	t.Fatal("NewEngine without Snapshots did not panic")
}
