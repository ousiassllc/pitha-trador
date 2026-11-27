package fullscanflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

// scan.full_scan_enabled: false (issue #652): no market-data job is enqueued.
func TestScheduler_EnqueueFullScan_DisabledEnqueuesNothing(t *testing.T) {
	db := newTestDB(t)
	instruments := market.NewInstrumentRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	mustCreateInstrument(t, instruments, "7203", true)

	s := scheduler.New(jobs, instruments, scheduler.WithFullScanDisabled())
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	count, err := s.EnqueueFullScan(context.Background(), now)
	if err != nil {
		t.Fatalf("EnqueueFullScan: %v", err)
	}
	if count != 0 {
		t.Errorf("EnqueueFullScan count = %d, want 0 when the full scan is disabled", count)
	}
	if _, err := jobs.ClaimNext(context.Background(), jobqueue.JobQueueMarketData, now); !errors.Is(err, jobqueue.ErrJobNotFound) {
		t.Errorf("ClaimNext(market-data) err = %v, want ErrJobNotFound", err)
	}
}
