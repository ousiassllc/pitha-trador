//go:build !race

package metalabeltests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// raceEnabled is false in a normal build (see race_test.go).
const raceEnabled = false

// A 366-day jev_models call (the longest period) over a realistic volume (about
// 100 decisions per trading day for a year) finishes inside the real read
// budget. A wall-clock claim, so it lives in the build without the race
// detector, where timing is stable enough; the -race build checks the
// answer and the index use instead (TestJevModelsOver366Days).
func TestJevModelsFitsTheReadBudgetOver366DaysWallClock(t *testing.T) {
	now := testkit.FixedNow()
	path, w := readinessDB(t)
	const n = 100 * 366 * 22 / 30
	bulkSeed(t, w, n, now.Add(-30*time.Minute), int(366*24*time.Hour/time.Second)/n, 16)
	srv := newTestServerWith(t, paperresultsmcp.Options{DBPath: path}, now)

	start := time.Now()
	r := srv.call(t, "jev_models", map[string]any{"from": "2025-10-18", "to": "2026-10-19"})
	elapsed := time.Since(start)
	var d jevModels
	r.data(t, "jev_models", &d)
	if elapsed >= paperresultsmcp.QueryTimeout {
		t.Errorf("366 days took %v, not under the %v read budget", elapsed, paperresultsmcp.QueryTimeout)
	}
	t.Logf("jev_models over 366 days: %d decisions, %d daily rows in %v (budget %v)", d.Models[0].Decisions, len(d.Daily), elapsed, paperresultsmcp.QueryTimeout)
}
