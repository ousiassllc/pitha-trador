package papersettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

type seedStore struct {
	rows   map[string]string
	setErr error
}

func (s *seedStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, nil
}

func (s *seedStore) Set(_ context.Context, key, value string, _ time.Time) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.rows[key] = value
	return nil
}

var seedNow = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

func TestSeedDefaults_FreshDatabaseStoresThePaperInitialValuesAndMarker(t *testing.T) {
	store := &seedStore{rows: map[string]string{}}
	if err := papersettings.SeedDefaults(context.Background(), store, seedNow); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	want := map[string]string{
		papersettings.KeyInitialCapitalJPY:       "500000",
		papersettings.KeyMaxPositionPerSymbolPct: "100",
		papersettings.KeyMaxTotalExposurePct:     "100",
		papersettings.SeededMarkerKey:            "true",
	}
	if len(store.rows) != len(want) {
		t.Errorf("rows = %v, want %v", store.rows, want)
	}
	for k, v := range want {
		if store.rows[k] != v {
			t.Errorf("%s = %q, want %q", k, store.rows[k], v)
		}
	}
}

func TestSeedDefaults_RunsOncePerDatabase(t *testing.T) {
	store := &seedStore{rows: map[string]string{}}
	ctx := context.Background()
	if err := papersettings.SeedDefaults(ctx, store, seedNow); err != nil {
		t.Fatal(err)
	}
	// The owner changed one value and deleted another: a restart must not bring them back.
	store.rows[papersettings.KeyInitialCapitalJPY] = "1000000"
	delete(store.rows, papersettings.KeyMaxTotalExposurePct)
	if err := papersettings.SeedDefaults(ctx, store, seedNow); err != nil {
		t.Fatal(err)
	}
	if store.rows[papersettings.KeyInitialCapitalJPY] != "1000000" {
		t.Errorf("capital = %q, want the owner's 1000000 kept", store.rows[papersettings.KeyInitialCapitalJPY])
	}
	if _, ok := store.rows[papersettings.KeyMaxTotalExposurePct]; ok {
		t.Error("a deleted row was seeded again")
	}
}

func TestSeedDefaults_ExistingRowOnlyWritesMarker(t *testing.T) {
	store := &seedStore{rows: map[string]string{papersettings.KeyMaxPositionPerSymbolPct: "5"}}
	if err := papersettings.SeedDefaults(context.Background(), store, seedNow); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 2 || store.rows[papersettings.KeyMaxPositionPerSymbolPct] != "5" || store.rows[papersettings.SeededMarkerKey] != "true" {
		t.Errorf("rows = %v, want the existing row untouched plus the marker", store.rows)
	}
}

func TestSeedDefaults_WriteFailureIsReturnedWithoutMarker(t *testing.T) {
	boom := errors.New("disk full")
	store := &seedStore{rows: map[string]string{}, setErr: boom}
	if err := papersettings.SeedDefaults(context.Background(), store, seedNow); !errors.Is(err, boom) {
		t.Fatalf("SeedDefaults error = %v, want %v", err, boom)
	}
	if _, ok := store.rows[papersettings.SeededMarkerKey]; ok {
		t.Error("marker written after a failed seed")
	}
}

func TestKeys_AreNotTheDailyGuardKey(t *testing.T) {
	if papersettings.IsKey("risk.operating_capital_jpy") {
		t.Error("the daily guard capital must stay a single stored row owned by dailyguardsettings")
	}
}
