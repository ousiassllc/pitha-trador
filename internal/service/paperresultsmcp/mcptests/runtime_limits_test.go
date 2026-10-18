package mcptests

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestFixedLimits(t *testing.T) {
	if paperresultsmcp.MaxConcurrent != 2 || paperresultsmcp.QueryTimeout != 3*time.Second {
		t.Fatalf("MaxConcurrent=%d QueryTimeout=%v", paperresultsmcp.MaxConcurrent, paperresultsmcp.QueryTimeout)
	}
}

// The 3rd concurrent tool call is concurrency_limited and starts no SQL; the
// two running calls still finish normally.
func TestConcurrencyLimit(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	counter.SetOnSQL(func(ctx context.Context, q string) error {
		if strings.Contains(q, "FROM positions") {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName,
		QueryTimeout: time.Minute})

	var wg sync.WaitGroup
	results := make([]response, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = srv.call(t, "paper_performance_summary", period)
		}()
	}
	<-entered // the first call is inside SQL
	deadline := time.Now().Add(10 * time.Second)
	for srv.ActiveCalls() < paperresultsmcp.MaxConcurrent { // the second call has taken its slot
		if time.Now().After(deadline) {
			t.Fatal("second call never took a concurrency slot")
		}
		time.Sleep(time.Millisecond)
	}
	before := counter.Statements.Load()

	srv.call(t, "paper_performance_summary", period).wantError(t, http.StatusTooManyRequests, paperresultsmcp.CodeConcurrencyLimited)
	if after := counter.Statements.Load(); after != before {
		t.Fatalf("the rejected call started SQL: %d -> %d statements", before, after)
	}

	close(release)
	wg.Wait()
	for i, r := range results {
		var d perfData
		r.data(t, "paper_performance_summary", &d)
		if d.ClosedTradeCount != 5 {
			t.Fatalf("running call %d: %+v", i, d)
		}
	}
	// The slots are free again.
	var d perfData
	srv.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", &d)
}

// A tool call that runs past the budget is query_timeout (504), cancelled,
// not retried.
func TestQueryTimeout(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	var mu sync.Mutex
	blocked := 0
	counter.SetOnSQL(func(ctx context.Context, q string) error {
		if strings.Contains(q, "FROM positions") {
			mu.Lock()
			blocked++
			mu.Unlock()
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName,
		QueryTimeout: 150 * time.Millisecond})
	start := time.Now()
	srv.call(t, "paper_performance_summary", period).wantError(t, http.StatusGatewayTimeout, paperresultsmcp.CodeQueryTimeout)
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout took far longer than the budget")
	}
	mu.Lock()
	defer mu.Unlock()
	if blocked != 1 {
		t.Fatalf("query ran %d times, want 1 (no automatic retry)", blocked)
	}
}

// A lock held past busy_timeout (1 s) is db_locked.
func TestDBLocked(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedPositions(t, w)
	// Rollback-journal mode: an EXCLUSIVE lock then blocks readers (in WAL
	// mode they would proceed).
	testkit.MustExec(t, w, `PRAGMA journal_mode=DELETE`)
	testkit.MustExec(t, w, `BEGIN EXCLUSIVE`)
	t.Cleanup(func() { _, _ = w.Exec(`ROLLBACK`) })

	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path})
	start := time.Now()
	srv.call(t, "paper_performance_summary", period).wantError(t, http.StatusServiceUnavailable, paperresultsmcp.CodeDBLocked)
	if d := time.Since(start); d < 800*time.Millisecond || d > 2900*time.Millisecond {
		t.Fatalf("db_locked after %v, want about busy_timeout (1s)", d)
	}
}
