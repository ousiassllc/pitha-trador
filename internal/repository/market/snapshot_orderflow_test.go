package market_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

// The order-flow features of migration 000032 round-trip, and a bar without
// them (OFI source off, no book) reads back as NULL/nil, never 0 (issue #750).
func TestSnapshotRepository_Insert_RoundTripsOrderFlowFeatures(t *testing.T) {
	conn := newTestDB(t)
	snapRepo := market.NewSnapshotRepository(conn)
	ctx := context.Background()
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(conn), "6758")

	for i, tc := range []struct {
		name string
		in   domain.Feature
	}{
		{"populated", domain.Feature{OrderFlowFeatures: domain.OrderFlowFeatures{
			OFIShort: new(120.0), OFIMid: new(-80.0), OFILong: new(0.0),
			OFIWindowShortSeconds: new(int64(10)), OFIWindowMidSeconds: new(int64(30)), OFIWindowLongSeconds: new(int64(60)),
			MicropriceDevBps: new(-1.25),
		}}},
		{"missing", domain.Feature{}},
	} {
		in := domain.Snapshot{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Price: 1000, RawDataJSON: `{}`,
			Timestamp: time.Date(2026, 9, 26, 1, 16+i, 0, 0, time.UTC), Feature: tc.in,
		}
		inserted, err := snapRepo.Insert(ctx, in)
		if err != nil {
			t.Fatalf("%s: Insert: %v", tc.name, err)
		}
		got, err := snapRepo.Get(ctx, inserted.ID)
		if err != nil {
			t.Fatalf("%s: Get: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got.Feature, tc.in) {
			t.Errorf("%s: Feature = %+v, want %+v", tc.name, got.Feature, tc.in)
		}
	}
}
