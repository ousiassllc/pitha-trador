package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// recordSleeps replaces s.sleep with a recorder and returns the slice of
// requested durations.
func recordSleeps(s *Service) *[]time.Duration {
	var got []time.Duration
	s.sleep = func(ctx context.Context, d time.Duration) error {
		got = append(got, d)
		return ctx.Err()
	}
	return &got
}

// The pause must outlast the driver's longest busy-handler sleep (100ms,
// modernc.org/sqlite lib _delays), or a waiting writer can still miss it.
func TestDefaultBatchPause_ExceedsBusyHandlerMaxSleep(t *testing.T) {
	if defaultBatchPause <= 100*time.Millisecond {
		t.Fatalf("defaultBatchPause = %v, want > 100ms", defaultBatchPause)
	}
	if got := New(nil, Policy{}).batchPause; got != defaultBatchPause {
		t.Errorf("New batchPause = %v, want %v", got, defaultBatchPause)
	}
}

func TestPurgeJobs_PausesAfterEveryFullBatchOnly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rows       int
		wantPauses int
	}{
		{"fewer than one batch", 1, 0},
		{"full batches then a partial one", 5, 2},
		{"exact multiple of the batch size", 4, 2}, // 2,2 then an empty batch
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db := newService(t, Policy{})
			s.batchSize = 2
			sleeps := recordSleeps(s)
			for range tc.rows {
				insertJob(t, db, "succeeded", 30)
			}

			if err := s.Purge(context.Background()); err != nil {
				t.Fatalf("Purge: %v", err)
			}
			if len(*sleeps) != tc.wantPauses {
				t.Fatalf("pauses = %d, want %d", len(*sleeps), tc.wantPauses)
			}
			for _, d := range *sleeps {
				if d != defaultBatchPause {
					t.Errorf("pause = %v, want %v", d, defaultBatchPause)
				}
			}
		})
	}
}

func TestPurgeSnapshots_PausesAfterEveryFullBatchOnly(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rows       int
		wantPauses int
	}{
		{"fewer than one batch", 1, 0},
		{"full batches then a partial one", 5, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db := newService(t, Policy{})
			s.batchSize = 2
			sleeps := recordSleeps(s)
			inst, err := market.NewInstrumentRepository(db).Create(context.Background(), domain.Instrument{
				Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			seedSnapshots(t, db, inst.ID, tc.rows, fixedNow.AddDate(0, 0, -120))

			if err := s.Purge(context.Background()); err != nil {
				t.Fatalf("Purge: %v", err)
			}
			if len(*sleeps) != tc.wantPauses {
				t.Fatalf("pauses = %d, want %d", len(*sleeps), tc.wantPauses)
			}
			if got := count(t, db, `SELECT COUNT(*) FROM market_snapshots`); got != 0 {
				t.Errorf("market_snapshots = %d, want 0", got)
			}
		})
	}
}

// A cancelled context interrupts the pause and stops the purge instead of
// finishing every remaining batch.
func TestPurge_StopsWhenPauseIsCancelled(t *testing.T) {
	s, db := newService(t, Policy{})
	s.batchSize = 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	for range 6 {
		insertJob(t, db, "succeeded", 30)
	}

	err := s.Purge(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Purge error = %v, want context.Canceled", err)
	}
	if got := jobCount(t, db, "succeeded"); got != 4 {
		t.Errorf("succeeded jobs = %d, want 4 (only the first batch deleted)", got)
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepContext = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepContext(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepContext on cancelled ctx = %v, want context.Canceled", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("sleepContext did not return promptly on a cancelled context")
	}
}
