package dailyguardsettings_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

type seedStore struct {
	rows   map[string]string
	getErr error
	setErr error
	okSets int // Sets that succeed before setErr applies
}

func (s *seedStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, s.getErr
}

func (s *seedStore) Set(_ context.Context, key, value string, _ time.Time) error {
	if s.setErr != nil {
		if s.okSets == 0 {
			return s.setErr
		}
		s.okSets--
	}
	s.rows[key] = value
	return nil
}

var seedNow = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

func TestSeedPaperDefaults_FreshDatabaseSeedsFiveValuesAndMarker(t *testing.T) {
	store := &seedStore{rows: map[string]string{}}
	if err := dailyguardsettings.SeedPaperDefaults(context.Background(), store, seedNow); err != nil {
		t.Fatalf("SeedPaperDefaults: %v", err)
	}
	want := map[string]string{
		dailyguardsettings.KeyOperatingCapitalJPY:   "500000",
		dailyguardsettings.KeyLossLimitPct:          "1",
		dailyguardsettings.KeyProfitLockTriggerPct:  "0.5",
		dailyguardsettings.KeyProfitLockGivebackPct: "50",
		dailyguardsettings.KeyMaxTrades:             "20",
		dailyguardsettings.SeededMarkerKey:          "true",
	}
	if len(store.rows) != len(want) {
		t.Fatalf("rows = %v, want %v", store.rows, want)
	}
	for k, v := range want {
		if store.rows[k] != v {
			t.Errorf("row %s = %q, want %q", k, store.rows[k], v)
		}
	}
	// What was seeded resolves to the documented guide values.
	r, err := dailyguardsettings.LoadResolved(context.Background(), mapStore{rows: store.rows})
	if err != nil {
		t.Fatalf("LoadResolved: %v", err)
	}
	if r.Limits != (dailyguardsettings.Limits{LossLimitJPY: 5000, ProfitLockTriggerJPY: 2500, ProfitLockGivebackPct: 50, MaxTrades: 20}) {
		t.Fatalf("seeded Limits = %+v", r.Limits)
	}
}

func TestSeedPaperDefaults_LegacyRowPresentWritesOnlyTheMarker(t *testing.T) {
	store := &seedStore{rows: map[string]string{dailyguardsettings.KeyLossLimitJPY: "50000"}}
	if err := dailyguardsettings.SeedPaperDefaults(context.Background(), store, seedNow); err != nil {
		t.Fatalf("SeedPaperDefaults: %v", err)
	}
	if len(store.rows) != 2 || store.rows[dailyguardsettings.KeyLossLimitJPY] != "50000" || store.rows[dailyguardsettings.SeededMarkerKey] != "true" {
		t.Fatalf("rows = %v, want the legacy row untouched plus the marker", store.rows)
	}
}

func TestSeedPaperDefaults_RunsOncePerDatabase(t *testing.T) {
	store := &seedStore{rows: map[string]string{}}
	ctx := context.Background()
	if err := dailyguardsettings.SeedPaperDefaults(ctx, store, seedNow); err != nil {
		t.Fatalf("first: %v", err)
	}
	// The owner customises one row and deletes the others.
	store.rows = map[string]string{
		dailyguardsettings.SeededMarkerKey:        "true",
		dailyguardsettings.KeyOperatingCapitalJPY: "800000",
	}
	if err := dailyguardsettings.SeedPaperDefaults(ctx, store, seedNow); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(store.rows) != 2 || store.rows[dailyguardsettings.KeyOperatingCapitalJPY] != "800000" {
		t.Fatalf("rows = %v, a later call must change nothing", store.rows)
	}
}

func TestSeedPaperDefaults_NeverOverwritesAnExistingRowOnPartialDatabase(t *testing.T) {
	store := &seedStore{rows: map[string]string{dailyguardsettings.KeyMaxTrades: "3"}}
	if err := dailyguardsettings.SeedPaperDefaults(context.Background(), store, seedNow); err != nil {
		t.Fatalf("SeedPaperDefaults: %v", err)
	}
	if store.rows[dailyguardsettings.KeyMaxTrades] != "3" || len(store.rows) != 2 {
		t.Fatalf("rows = %v", store.rows)
	}
}

func TestSeedPaperDefaults_StoreFailuresAreErrors(t *testing.T) {
	if err := dailyguardsettings.SeedPaperDefaults(context.Background(), &seedStore{rows: map[string]string{}, getErr: errors.New("db down")}, seedNow); err == nil {
		t.Error("a read failure was swallowed")
	}
	if err := dailyguardsettings.SeedPaperDefaults(context.Background(), &seedStore{rows: map[string]string{}, setErr: errors.New("db down")}, seedNow); err == nil {
		t.Error("a write failure was swallowed")
	}
}

// A failure after some values were stored says it was a partial seed; a failure
// of the very first write stores nothing and does not.
func TestSeedPaperDefaults_PartialWriteErrorSaysSo(t *testing.T) {
	store := &seedStore{rows: map[string]string{}, setErr: errors.New("db down"), okSets: 2}
	err := dailyguardsettings.SeedPaperDefaults(context.Background(), store, seedNow)
	if err == nil || !strings.Contains(err.Error(), "一部の初期値のみ保存済み") {
		t.Fatalf("err = %v, want a partial-seed message", err)
	}
	if _, ok := store.rows[dailyguardsettings.SeededMarkerKey]; ok || len(store.rows) != 2 {
		t.Fatalf("rows = %v, want two seeded rows and no marker", store.rows)
	}

	store = &seedStore{rows: map[string]string{}, setErr: errors.New("db down")}
	if err := dailyguardsettings.SeedPaperDefaults(context.Background(), store, seedNow); err == nil || strings.Contains(err.Error(), "一部") {
		t.Fatalf("first-write failure err = %v, want no partial-seed message", err)
	}
}
