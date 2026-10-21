package bucketsettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
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

func TestSeedDefaults_FreshDatabaseGivesStockTheWholeCapitalAndBondETFZero(t *testing.T) {
	store := &seedStore{rows: map[string]string{}}
	if err := bucketsettings.SeedDefaults(context.Background(), store, 500000, seedNow); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		bucketsettings.KeyStockBudgetJPY:   "500000",
		bucketsettings.KeyBondETFBudgetJPY: "0",
		bucketsettings.SeededMarkerKey:     "true",
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
	ctx := context.Background()
	store := &seedStore{rows: map[string]string{}}
	if err := bucketsettings.SeedDefaults(ctx, store, 500000, seedNow); err != nil {
		t.Fatal(err)
	}
	// The owner changed one value and deleted the other: a restart must not bring them back.
	store.rows[bucketsettings.KeyStockBudgetJPY] = "100000"
	delete(store.rows, bucketsettings.KeyBondETFBudgetJPY)
	if err := bucketsettings.SeedDefaults(ctx, store, 900000, seedNow); err != nil {
		t.Fatal(err)
	}
	if store.rows[bucketsettings.KeyStockBudgetJPY] != "100000" {
		t.Errorf("stock = %q, want the owner's 100000 kept", store.rows[bucketsettings.KeyStockBudgetJPY])
	}
	if _, ok := store.rows[bucketsettings.KeyBondETFBudgetJPY]; ok {
		t.Error("bond_etf row came back after the owner deleted it")
	}
}

func TestSeedDefaults_NeverOverwritesAStoredRow(t *testing.T) {
	store := &seedStore{rows: map[string]string{bucketsettings.KeyStockBudgetJPY: "123"}}
	if err := bucketsettings.SeedDefaults(context.Background(), store, 500000, seedNow); err != nil {
		t.Fatal(err)
	}
	if store.rows[bucketsettings.KeyStockBudgetJPY] != "123" {
		t.Errorf("stock = %q, want the stored 123", store.rows[bucketsettings.KeyStockBudgetJPY])
	}
	if _, ok := store.rows[bucketsettings.KeyBondETFBudgetJPY]; ok {
		t.Error("bond_etf was seeded although a bucket row already existed")
	}
	if store.rows[bucketsettings.SeededMarkerKey] != "true" {
		t.Error("marker not written")
	}
}

func TestSeedDefaults_SetFailureIsReturnedAndLeavesNoMarker(t *testing.T) {
	boom := errors.New("boom")
	store := &seedStore{rows: map[string]string{}, setErr: boom}
	if err := bucketsettings.SeedDefaults(context.Background(), store, 500000, seedNow); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if _, ok := store.rows[bucketsettings.SeededMarkerKey]; ok {
		t.Error("marker written although seeding failed")
	}
}
