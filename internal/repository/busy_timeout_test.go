package repository_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
)

// TestOpen_ConcurrentWritersDoNotHitSQLiteBusy exercises two independent
// *sql.DB handles (as produced by repository.Open) against the same
// on-disk database file, mirroring how two goroutines/packages
// (e.g. internal/service/scheduler's dispatcher and enqueuer) share a
// single SQLite database in production. Without a configured
// PRAGMA busy_timeout, modernc.org/sqlite returns SQLITE_BUSY immediately
// whenever a writer collides with another writer holding the WAL lock,
// even though WAL mode itself permits concurrent readers/writer (see
// issue #39). This test drives enough concurrent writers across both
// handles to reliably surface that collision when busy_timeout is unset,
// and asserts that every write succeeds once busy_timeout is configured.
func TestOpen_ConcurrentWritersDoNotHitSQLiteBusy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pitha.db")

	connA, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) connA: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := connA.Close(); err != nil {
			t.Errorf("close connA: %v", err)
		}
	})

	connB, err := repository.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) connB: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := connB.Close(); err != nil {
			t.Errorf("close connB: %v", err)
		}
	})

	const (
		workersPerConn      = 20
		insertsPerGoroutine = 10
	)

	var wg sync.WaitGroup
	errCh := make(chan error, 2*workersPerConn*insertsPerGoroutine)

	insert := func(conn *sql.DB, worker, i int) error {
		now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		_, err := conn.Exec(
			`INSERT INTO jobs (queue, payload_json, status, attempts, scheduled_at, created_at)
			 VALUES (?, ?, 'pending', 0, ?, ?)`,
			fmt.Sprintf("busy-timeout-test-%d", worker), "{}", now, now,
		)
		if err != nil {
			return fmt.Errorf("insert (worker=%d, i=%d): %w", worker, i, err)
		}
		return nil
	}

	for _, conn := range []*sql.DB{connA, connB} {
		for w := 0; w < workersPerConn; w++ {
			wg.Add(1)
			go func(conn *sql.DB, worker int) {
				defer wg.Done()
				for i := 0; i < insertsPerGoroutine; i++ {
					if err := insert(conn, worker, i); err != nil {
						errCh <- err
						return
					}
				}
			}(conn, w)
		}
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		t.Fatalf("expected all concurrent inserts to succeed (busy_timeout should absorb WAL writer contention), got %d error(s), first: %v", len(errs), errs[0])
	}

	var count int
	if err := connA.QueryRow("SELECT COUNT(*) FROM jobs WHERE queue LIKE 'busy-timeout-test-%'").Scan(&count); err != nil {
		t.Fatalf("count inserted jobs: %v", err)
	}
	if want := 2 * workersPerConn * insertsPerGoroutine; count != want {
		t.Fatalf("expected %d inserted jobs, got %d", want, count)
	}
}
