package marketdatajob

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// universeSize is non-functional.md §2.3's upper bound on the symbol master.
const universeSize = 4000

// seedUniverse bulk-inserts n active instruments (symbols "1000".."1000+n-1").
func seedUniverse(t testing.TB, env testEnv, n int) {
	t.Helper()
	tx, err := env.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := range n {
		sym := fmt.Sprintf("%d", 1000+i)
		if _, err := tx.Exec(`INSERT INTO instruments (symbol, name, market, is_active) VALUES (?, ?, 'TSE Prime', 1)`, sym, sym+" Inc."); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert instrument %s: %v", sym, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// runFullScanCycle is one scheduler cycle as the production wiring runs it:
// EnqueueFullScan, then the market-data worker draining the queue one job
// at a time (ClaimNext -> HandleMarketData -> MarkSucceeded, exactly
// scheduler.processNext). It returns the jobs processed.
func runFullScanCycle(t testing.TB, env testEnv, now time.Time) int {
	t.Helper()
	ctx := context.Background()
	enqueued, err := env.Scheduler.EnqueueFullScan(ctx, now)
	if err != nil {
		t.Fatalf("EnqueueFullScan: %v", err)
	}
	processed := 0
	for {
		job, err := env.Jobs.ClaimNext(ctx, jobqueue.JobQueueMarketData, time.Now().UTC())
		if errors.Is(err, jobqueue.ErrJobNotFound) {
			break
		}
		if err != nil {
			t.Fatalf("ClaimNext: %v", err)
		}
		if err := env.HandleMarketData(ctx, job); err != nil {
			t.Fatalf("HandleMarketData: %v", err)
		}
		if err := env.Jobs.MarkSucceeded(ctx, job.ID, time.Now().UTC()); err != nil {
			t.Fatalf("MarkSucceeded: %v", err)
		}
		processed++
	}
	if processed != enqueued {
		t.Fatalf("processed %d jobs, EnqueueFullScan enqueued %d", processed, enqueued)
	}
	return processed
}

// A full-scan cycle processes exactly one market-data job per active
// instrument and enqueues nothing on feature-calc: the worker drains what
// EnqueueFullScan produced, one job per symbol. (The 4,000-symbol timing is
// BenchmarkFullScanCycle; wall-clock thresholds are not
// asserted here because they depend on the host's disk.)
func TestFullScanCycle_OneMarketDataJobPerInstrument(t *testing.T) {
	env := newTestEnv(t)
	env.Fake.board = marketdata.Board{CurrentPrice: 2500, VWAP: 2490, TradingVolume: 1000000, TradingValue: 2.49e9}
	seedUniverse(t, env, 50)

	if n := runFullScanCycle(t, env, time.Now().UTC()); n != 50 {
		t.Fatalf("cycle processed %d jobs, want 50 (one market-data job per instrument)", n)
	}
	if _, err := env.Jobs.ClaimNext(context.Background(), jobqueue.JobQueueFeatureCalc, time.Now().UTC()); !errors.Is(err, jobqueue.ErrJobNotFound) {
		t.Fatalf("ClaimNext(feature-calc) err = %v, want ErrJobNotFound (full scan must not feed feature-calc)", err)
	}
}

// BenchmarkFullScanCycle measures one full-scan cycle with an instantaneous
// board source, i.e. the fixed per-symbol cost of the single market-data
// worker (ClaimNext, feature computation and snapshot save, MarkSucceeded;
// all SQLite commits). The figures in non-functional.md §2.3 come from it:
//
//	go test ./internal/bootstrap/marketdatajob -run '^$' -bench FullScanCycle -benchtime 1x
func BenchmarkFullScanCycle(b *testing.B) {
	for _, size := range []int{500, 1000, universeSize} {
		b.Run(fmt.Sprintf("Universe%d", size), func(b *testing.B) {
			env := newTestEnv(b)
			env.Fake.board = marketdata.Board{CurrentPrice: 2500, VWAP: 2490, TradingVolume: 1000000, TradingValue: 2.49e9}
			seedUniverse(b, env, size)
			b.ResetTimer()
			for range b.N {
				runFullScanCycle(b, env, time.Now().UTC())
			}
			b.ReportMetric(float64(b.Elapsed().Milliseconds())/float64(b.N*size), "ms/symbol")
		})
	}
}
