package researchfxjob

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/fx/gmo"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
)

type failingSettings struct{ err error }

func (f failingSettings) Get(context.Context, string) (string, bool, error) { return "", false, f.err }

// A switch that cannot be read is never "on": the run is skipped (no request, no
// write, not a failure the runner would count and notify about), whatever the
// database would have said.
func TestJob_AnUnreadableSwitchIsSkippedNotSwitchedOnNorFailed(t *testing.T) {
	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	e.job.settings = failingSettings{errors.New("database is locked")}
	tables := []string{"research_fx_candles", "research_fx_days", "runtime_settings"}
	before := map[string]int{}
	for _, tbl := range tables {
		before[tbl] = e.count(t, `SELECT COUNT(*) FROM `+tbl)
	}

	if err := e.job.Run(context.Background()); !errors.Is(err, maintenance.ErrSkipped) {
		t.Fatalf("Run = %v, want maintenance.ErrSkipped", err)
	}
	if n := e.gmo.count(); n != 0 {
		t.Fatalf("the mock GMO server saw %d requests after a failed settings read, want 0", n)
	}
	for _, tbl := range tables {
		if got := e.count(t, `SELECT COUNT(*) FROM `+tbl); got != before[tbl] {
			t.Errorf("%s rows %d -> %d after a failed settings read", tbl, before[tbl], got)
		}
	}
}

// A GMO server that never answers ends at the chunk deadline: Run returns an
// error (no marker, retried by the next run) instead of holding the maintenance
// runner's lock; the pause between the adapter's attempts uses a fake clock.
func TestJob_AHangingGMOEndsAtTheChunkDeadlineWithoutAMarker(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-time.After(time.Minute):
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	e := newEnv(t)
	e.set(t, researchfxsettings.KeyEnabled, "1")
	clk := &fakeClock{now: e.now}
	e.job = New(e.db, e.settings, WithBaseURL(srv.URL), WithGMOOptions(gmo.WithClock(clk)),
		WithClock(func() time.Time { return e.now }), WithoutPace(), WithChunkTimeout(50*time.Millisecond))

	err := e.job.Run(context.Background())
	if err == nil || errors.Is(err, maintenance.ErrSkipped) || errors.Is(err, maintenance.ErrMore) {
		t.Fatalf("Run = %v, want a failure at the chunk deadline", err)
	}
	if hits.Load() == 0 {
		t.Fatal("the hanging server was never reached")
	}
	if got := e.count(t, `SELECT COUNT(*) FROM research_fx_days`); got != 0 {
		t.Fatalf("markers = %d after a stuck day, want 0", got)
	}
}
