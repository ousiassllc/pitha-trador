package market_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func mustCreateInstrument(t *testing.T, repo *market.InstrumentRepository, symbol string) domain.Instrument {
	t.Helper()
	inst, err := repo.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: "Test " + symbol, Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}

func TestSnapshotRepository_InsertAndGet_HandlesNullBoardColumns(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()

	inst := mustCreateInstrument(t, instRepo, "7203")

	inserted, err := snapRepo.Insert(ctx, domain.Snapshot{
		InstrumentID: inst.ID,
		Symbol:       inst.Symbol,
		Timestamp:    time.Date(2026, 9, 26, 1, 15, 0, 0, time.UTC),
		Price:        2500.5,
		// Bid/Ask/SpreadBps deliberately left nil: 板情報取得不可時の欠損値
		// （requirements/functional.md FR-FE-2）
		Bid:       nil,
		Ask:       nil,
		SpreadBps: nil,
		Volume:    10000,
		Turnover:  25_005_000,
		Feature: domain.Feature{
			// Return1m/5m/15m等も起動直後は算出不可のためnil
			VWAP:           2498.0,
			PriceVsVWAPBps: 10.0,
		},
		RawDataJSON: `{"raw":true}`,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if inserted.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}

	got, err := snapRepo.Get(ctx, inserted.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", inserted.ID, err)
	}
	if got.Bid != nil || got.Ask != nil || got.SpreadBps != nil {
		t.Fatalf("Get(%d) board columns = bid=%v ask=%v spread_bps=%v, want all nil",
			inserted.ID, got.Bid, got.Ask, got.SpreadBps)
	}
	if got.Feature.Return1m != nil {
		t.Fatalf("Get(%d).Feature.Return1m = %v, want nil", inserted.ID, got.Feature.Return1m)
	}
	if got.Feature.VWAP != 2498.0 || got.Feature.PriceVsVWAPBps != 10.0 {
		t.Fatalf("Get(%d).Feature = %+v, want VWAP=2498.0 PriceVsVWAPBps=10.0", inserted.ID, got.Feature)
	}
	if !got.Timestamp.Equal(time.Date(2026, 9, 26, 1, 15, 0, 0, time.UTC)) {
		t.Fatalf("Get(%d).Timestamp = %v, want 2026-09-26T01:15:00Z", inserted.ID, got.Timestamp)
	}
}

func TestSnapshotRepository_Insert_PopulatesBoardColumnsWhenPresent(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()

	inst := mustCreateInstrument(t, instRepo, "6758")

	inserted, err := snapRepo.Insert(ctx, domain.Snapshot{
		InstrumentID: inst.ID,
		Symbol:       inst.Symbol,
		Timestamp:    time.Date(2026, 9, 26, 1, 16, 0, 0, time.UTC),
		Price:        1000,
		Bid:          floatPtr(999.5),
		Ask:          floatPtr(1000.5),
		SpreadBps:    floatPtr(10.0),
		Volume:       500,
		Turnover:     500_000,
		Feature: domain.Feature{
			Return1m:           floatPtr(0.01),
			OrderbookImbalance: floatPtr(0.2),
			VWAP:               999.0,
			PriceVsVWAPBps:     10.0,
		},
		RawDataJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := snapRepo.Get(ctx, inserted.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", inserted.ID, err)
	}
	if got.Bid == nil || *got.Bid != 999.5 {
		t.Fatalf("Get(%d).Bid = %v, want 999.5", inserted.ID, got.Bid)
	}
	if got.Feature.Return1m == nil || *got.Feature.Return1m != 0.01 {
		t.Fatalf("Get(%d).Feature.Return1m = %v, want 0.01", inserted.ID, got.Feature.Return1m)
	}
	if got.Feature.OrderbookImbalance == nil || *got.Feature.OrderbookImbalance != 0.2 {
		t.Fatalf("Get(%d).Feature.OrderbookImbalance = %v, want 0.2", inserted.ID, got.Feature.OrderbookImbalance)
	}
}

func TestSnapshotRepository_InsertBatch_CommitsAllInOneTransaction(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()

	instA := mustCreateInstrument(t, instRepo, "1301")
	instB := mustCreateInstrument(t, instRepo, "1332")
	ts := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)

	batch := []domain.Snapshot{
		{InstrumentID: instA.ID, Symbol: instA.Symbol, Timestamp: ts, Price: 100, Volume: 1, Turnover: 100, Feature: domain.Feature{VWAP: 100, PriceVsVWAPBps: 0}, RawDataJSON: "{}"},
		{InstrumentID: instB.ID, Symbol: instB.Symbol, Timestamp: ts, Price: 200, Volume: 1, Turnover: 200, Feature: domain.Feature{VWAP: 200, PriceVsVWAPBps: 0}, RawDataJSON: "{}"},
	}

	inserted, err := snapRepo.InsertBatch(ctx, batch)
	if err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if len(inserted) != 2 {
		t.Fatalf("InsertBatch() returned %d snapshots, want 2", len(inserted))
	}

	rows, err := snapRepo.ListByInstrument(ctx, instA.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument(%d): %v", instA.ID, err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListByInstrument(%d) = %d rows, want 1", instA.ID, len(rows))
	}
}

func TestSnapshotRepository_InsertBatch_RollsBackOnUniqueViolation(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()

	inst := mustCreateInstrument(t, instRepo, "1305")
	ts := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)

	// UNIQUE (instrument_id, timestamp): the second row in the batch
	// duplicates the first's (instrument_id, timestamp), so the whole
	// batch must fail and neither row must be committed.
	batch := []domain.Snapshot{
		{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, Price: 100, Volume: 1, Turnover: 100, Feature: domain.Feature{VWAP: 100}, RawDataJSON: "{}"},
		{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, Price: 101, Volume: 1, Turnover: 101, Feature: domain.Feature{VWAP: 101}, RawDataJSON: "{}"},
	}

	if _, err := snapRepo.InsertBatch(ctx, batch); err == nil {
		t.Fatalf("InsertBatch() with duplicate (instrument_id, timestamp) succeeded, want UNIQUE constraint error")
	}

	rows, err := snapRepo.ListByInstrument(ctx, inst.ID, 10)
	if err != nil {
		t.Fatalf("ListByInstrument(%d): %v", inst.ID, err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListByInstrument(%d) = %d rows after rolled-back batch, want 0", inst.ID, len(rows))
	}
}

func TestSnapshotRepository_ListByInstrumentRange_AscendingWithinBounds(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()

	inst := mustCreateInstrument(t, instRepo, "1306")
	base := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)

	// Inserted out of chronological order and spanning outside the
	// queried range, so the test also proves ListByInstrumentRange
	// re-orders ascending and excludes bars outside [from, to) - the
	// ordering internal/service/backtest's Walk Forward replay depends
	// on (FR-BT-2/FR-BT-3).
	for i, minutesOffset := range []int{4, 0, 2, -1, 5} {
		ts := base.Add(time.Duration(minutesOffset) * time.Minute)
		snap := domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts,
			Price: 100 + float64(i), Volume: 1, Turnover: 100, Feature: domain.Feature{VWAP: 100}, RawDataJSON: "{}",
		}
		if _, err := snapRepo.Insert(ctx, snap); err != nil {
			t.Fatalf("Insert(%s): %v", ts, err)
		}
	}

	from := base
	to := base.Add(5 * time.Minute)
	rows, err := snapRepo.ListByInstrumentRange(ctx, inst.ID, from, to)
	if err != nil {
		t.Fatalf("ListByInstrumentRange(%d, %s, %s): %v", inst.ID, from, to, err)
	}

	wantOffsets := []int{0, 2, 4}
	if len(rows) != len(wantOffsets) {
		t.Fatalf("len(rows) = %d, want %d (offsets -1 and 5 fall outside [from, to))", len(rows), len(wantOffsets))
	}
	for i, want := range wantOffsets {
		wantTS := base.Add(time.Duration(want) * time.Minute)
		if !rows[i].Timestamp.Equal(wantTS) {
			t.Errorf("rows[%d].Timestamp = %s, want %s (ascending order)", i, rows[i].Timestamp, wantTS)
		}
	}
}
