package jobqueue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// TestJobRepository_ClaimNext_EmptyPollTakesNoWriteLock pins issue #541:
// the scheduler polls every queue every 200ms even when idle, so a
// ClaimNext that finds nothing due must not acquire the database write
// lock (it used to BEGIN IMMEDIATE every time). With another connection
// holding the write lock, an empty poll still answers ErrJobNotFound
// immediately instead of waiting out busy_timeout.
func TestJobRepository_ClaimNext_EmptyPollTakesNoWriteLock(t *testing.T) {
	db := newTestDB(t)
	repo := jobqueue.NewJobRepository(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

	// A pending job that is not yet due must not be claimed either.
	if _, err := repo.Enqueue(ctx, jobqueue.JobQueueJevScout, "{}", now.Add(time.Hour)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer func() { _ = holder.Close() }()
	if _, err := holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("hold write lock: %v", err)
	}
	defer func() { _, _ = holder.ExecContext(ctx, "ROLLBACK") }()

	pollCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, queue := range []string{jobqueue.JobQueueMarketData, jobqueue.JobQueueJevScout} {
		if _, err := repo.ClaimNext(pollCtx, queue, now); !errors.Is(err, jobqueue.ErrJobNotFound) {
			t.Fatalf("ClaimNext(%s) with nothing due = %v, want ErrJobNotFound without blocking on the write lock", queue, err)
		}
	}
}
