package updatecheck_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// TestScheduler_Start_RetriesFailedStartupUpdateCheck regresses issue #240
// through the real Scheduler.Start: a startup check that failed must be
// retried within the backoff, not left to the @every 6h cron tick.
func TestScheduler_Start_RetriesFailedStartupUpdateCheck(t *testing.T) {
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha_test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	checker := &scriptedChecker{errs: []error{errors.New("network unreachable"), errors.New("rate limited")}}

	s := scheduler.New(jobqueue.NewJobRepository(db), market.NewInstrumentRepository(db),
		scheduler.WithUpdateChecker(checker),
		scheduler.WithUpdateRetryBackoff(5*time.Millisecond, 20*time.Millisecond))
	// A 1h full-scan interval keeps that unrelated trigger from firing.
	if err := s.Start(context.Background(), 1*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for checker.Calls() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("update checker called %d time(s), want 3 well before the next @every 6h cron tick", checker.Calls())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
