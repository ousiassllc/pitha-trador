package bootstrap

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
)

// The ranking-based watch is the default (scan.full_scan_enabled omitted or
// false): the watcher is built and the PUSH feed and candidate refresh follow
// its watch list. An explicit true restores the full scan and builds none.
func TestBuildServices_RankingWatchIsDefaultAndFullScanOptIn(t *testing.T) {
	yes, no := true, false
	for name, tc := range map[string]struct {
		enabled   *bool
		wantWatch bool
	}{
		"omitted": {nil, true},
		"false":   {&no, true},
		"true":    {&yes, false},
	} {
		t.Run(name, func(t *testing.T) {
			state, err := Run(Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			t.Cleanup(func() { _ = state.Close() })
			state.Strategy.Scan.FullScanEnabled = tc.enabled
			svc := BuildServices(state, config.Secrets{KabuAPIPassword: "pw", JevBaseURL: "http://127.0.0.1:1"},
				WithExecutionClock(func() time.Time { return tradingHours }))

			if got := svc.rankingWatcher != nil; got != tc.wantWatch {
				t.Errorf("ranking watcher built = %v, want %v", got, tc.wantWatch)
			}
			if got := svc.candidates.Watch != nil; got != tc.wantWatch {
				t.Errorf("candidate refresh follows the watch list = %v, want %v", got, tc.wantWatch)
			}
		})
	}
}
