package market_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func seedBars(t *testing.T, repo *market.SnapshotRepository, inst domain.Instrument, n int) {
	t.Helper()
	base := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	for i := range n {
		_, err := repo.Insert(context.Background(), domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol,
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Price:     float64(100 + i), Volume: int64(i), Turnover: float64(i),
			Feature:     domain.Feature{VWAP: float64(100 + i), Return1m: floatPtr(0.1 * float64(i))},
			RawDataJSON: `{"board":"large"}`,
		})
		if err != nil {
			t.Fatalf("Insert bar %d for %s: %v", i, inst.Symbol, err)
		}
	}
}

// ListHistoryByInstruments must return exactly what ListByInstrument returns
// per instrument (same rows, same most-recent-first order) minus
// raw_data_json, for instruments with more than, exactly, fewer than the
// limit and zero bars.
func TestSnapshotRepository_ListHistoryByInstruments_MatchesListByInstrumentWithoutRawData(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()
	const limit = 4

	bars := map[string]int{"1001": 9, "1002": limit, "1003": 2, "1004": 0}
	var ids []int64
	var insts []domain.Instrument
	for _, sym := range []string{"1001", "1002", "1003", "1004"} {
		inst := mustCreateInstrument(t, instRepo, sym)
		seedBars(t, snapRepo, inst, bars[sym])
		insts = append(insts, inst)
		ids = append(ids, inst.ID)
	}

	got, err := snapRepo.ListHistoryByInstruments(ctx, ids, limit)
	if err != nil {
		t.Fatalf("ListHistoryByInstruments: %v", err)
	}

	for _, inst := range insts {
		want, err := snapRepo.ListByInstrument(ctx, inst.ID, limit)
		if err != nil {
			t.Fatal(err)
		}
		for i := range want {
			want[i].RawDataJSON = ""
		}
		if len(want) == 0 {
			if _, ok := got[inst.ID]; ok {
				t.Errorf("%s has no bars but has a map entry", inst.Symbol)
			}
			continue
		}
		if !reflect.DeepEqual(got[inst.ID], want) {
			t.Errorf("%s history = %+v, want %+v", inst.Symbol, got[inst.ID], want)
		}
		single, err := snapRepo.ListHistoryByInstrument(ctx, inst.ID, limit)
		if err != nil || !reflect.DeepEqual(single, want) {
			t.Errorf("%s ListHistoryByInstrument = %+v (err %v), want %+v", inst.Symbol, single, err, want)
		}
	}
	if n := len(got[ids[0]]); n != limit {
		t.Errorf("1001 rows = %d, want %d", n, limit)
	}
}

// ListHistoryByInstrumentRange/Before must return exactly what the raw
// variants return minus raw_data_json (issue #597: the backtest replay
// never reads the ~1-2KB board).
func TestSnapshotRepository_ListHistoryRangeAndBefore_MatchRawVariantsWithoutRawData(t *testing.T) {
	conn := newTestDB(t)
	instRepo := market.NewInstrumentRepository(conn)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()
	inst := mustCreateInstrument(t, instRepo, "1001")
	seedBars(t, snapRepo, inst, 9)
	base := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	from, to := base.Add(2*time.Minute), base.Add(7*time.Minute)

	wantRange, err := snapRepo.ListByInstrumentRange(ctx, inst.ID, from, to)
	if err != nil || len(wantRange) != 5 {
		t.Fatalf("ListByInstrumentRange = %d rows, err %v; want 5", len(wantRange), err)
	}
	gotRange, err := snapRepo.ListHistoryByInstrumentRange(ctx, inst.ID, from, to)
	if err != nil {
		t.Fatalf("ListHistoryByInstrumentRange: %v", err)
	}
	wantBefore, err := snapRepo.ListByInstrumentBefore(ctx, inst.ID, from, 5)
	if err != nil || len(wantBefore) != 2 {
		t.Fatalf("ListByInstrumentBefore = %d rows, err %v; want 2", len(wantBefore), err)
	}
	gotBefore, err := snapRepo.ListHistoryByInstrumentBefore(ctx, inst.ID, from, 5)
	if err != nil {
		t.Fatalf("ListHistoryByInstrumentBefore: %v", err)
	}

	for _, rows := range [][]domain.Snapshot{wantRange, wantBefore} {
		for i := range rows {
			if rows[i].RawDataJSON == "" {
				t.Fatal("raw variant returned empty RawDataJSON; the fixture does not exercise the exclusion")
			}
			rows[i].RawDataJSON = ""
		}
	}
	if !reflect.DeepEqual(gotRange, wantRange) {
		t.Errorf("ListHistoryByInstrumentRange = %+v, want %+v", gotRange, wantRange)
	}
	if !reflect.DeepEqual(gotBefore, wantBefore) {
		t.Errorf("ListHistoryByInstrumentBefore = %+v, want %+v", gotBefore, wantBefore)
	}
}
