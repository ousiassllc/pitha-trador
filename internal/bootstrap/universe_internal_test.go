package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
)

// TestSyncUniverse_CleanDBGetsUniverseSoFullScanEnqueuesJobs is issue #389's
// acceptance path: a clean DB plus the universe CSV yields stocks and index
// instruments, and the full scan then enqueues one market-data job per
// scanned instrument.
func TestSyncUniverse_CleanDBGetsUniverseSoFullScanEnqueuesJobs(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "universe.csv")
	body := "symbol,name,market,sector,kind\n7203,トヨタ自動車,TSE Prime,輸送用機器,stock\n6758,ソニーグループ,TSE Prime,電気機器,stock\n101,TOPIX,INDEX,,market_index\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvUniversePath, path)

	svc.syncUniverse(ctx)
	svc.syncUniverse(ctx) // restart: must not duplicate

	stocks, err := svc.Instruments.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil || len(stocks) != 2 {
		t.Fatalf("stocks = %d (err %v), want 2", len(stocks), err)
	}
	if indexes, err := svc.Instruments.ListActiveByKind(ctx, domain.InstrumentKindMarketIndex); err != nil || len(indexes) != 1 {
		t.Fatalf("market indexes = %d (err %v), want 1", len(indexes), err)
	}

	inSession := time.Date(2026, 9, 29, 10, 0, 0, 0, time.FixedZone("JST", 9*3600))
	n, err := svc.Scheduler.EnqueueFullScan(ctx, inSession)
	if err != nil || n != 3 {
		t.Fatalf("EnqueueFullScan = (%d, %v), want 3 instruments", n, err)
	}
	counts, err := svc.Jobs.QueueCounts(ctx, inSession)
	if err != nil {
		t.Fatalf("QueueCounts: %v", err)
	}
	for _, c := range counts {
		if c.Queue == jobqueue.JobQueueMarketData && c.Pending == 3 {
			return
		}
	}
	t.Fatalf("queue counts = %+v, want 3 pending market-data jobs", counts)
}

// A broken universe file must not wipe or alter what the DB already holds.
func TestSyncUniverse_InvalidFileKeepsExistingInstruments(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()
	mustCreateInstrument(t, svc, "7203")
	path := filepath.Join(t.TempDir(), "universe.csv")
	if err := os.WriteFile(path, []byte("symbol,name,market,kind\n6758,ソニー,TSE Prime,etf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvUniversePath, path)

	svc.syncUniverse(ctx)

	active, err := svc.Instruments.ListActive(ctx)
	if err != nil || len(active) != 1 || active[0].Symbol != "7203" {
		t.Fatalf("ListActive = %+v (err %v), want only the pre-existing 7203", active, err)
	}
}
