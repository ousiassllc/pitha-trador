package bucketsettings_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

type seedStore struct {
	rows   map[string]string
	setErr error
}

func (s *seedStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, nil
}

func (s *seedStore) SetIfAbsent(_ context.Context, key, value string, _ time.Time) (bool, error) {
	if s.setErr != nil {
		return false, s.setErr
	}
	if _, ok := s.rows[key]; ok {
		return false, nil
	}
	s.rows[key] = value
	return true, nil
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

// racingStore is a real runtime_settings repository whose Get calls onGet after
// reading, so a test can run another writer exactly between the seed's read and
// its write - deterministically, without a sleep.
type racingStore struct {
	*system.RuntimeSettingsRepository
	onGet func(key string)
}

func (s *racingStore) Get(ctx context.Context, key string) (string, bool, error) {
	v, ok, err := s.RuntimeSettingsRepository.Get(ctx, key)
	if s.onGet != nil {
		s.onGet(key)
	}
	return v, ok, err
}

func newRacingStore(t *testing.T) (*racingStore, *system.RuntimeSettingsRepository) {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	repo := system.NewRuntimeSettingsRepository(conn)
	return &racingStore{RuntimeSettingsRepository: repo}, repo
}

// Issue #837: a budget the Settings screen or the MCP stores after the seed
// decided "nothing stored" - between its read and its write - survives the seed.
func TestSeedDefaults_ConcurrentSaveIsNotOverwrittenOnARealDatabase(t *testing.T) {
	ctx := context.Background()
	store, repo := newRacingStore(t)
	saved := false
	store.onGet = func(key string) {
		// The seed's last row read (nothing stored yet) is followed by the save.
		if key != bucketsettings.KeyBondETFBudgetJPY || saved {
			return
		}
		saved = true
		if err := repo.Set(ctx, bucketsettings.KeyStockBudgetJPY, "123456", seedNow.Add(time.Second)); err != nil {
			t.Errorf("concurrent save: %v", err)
		}
	}
	if err := bucketsettings.SeedDefaults(ctx, store, 500000, seedNow); err != nil {
		t.Fatal(err)
	}
	if !saved {
		t.Fatal("the concurrent save never ran")
	}
	if got, ok, err := repo.Get(ctx, bucketsettings.KeyStockBudgetJPY); err != nil || !ok || got != "123456" {
		t.Errorf("stock budget = %q (stored=%v, err=%v), want the saved 123456 kept", got, ok, err)
	}
	if got, ok, err := repo.Get(ctx, bucketsettings.SeededMarkerKey); err != nil || !ok || got != "true" {
		t.Errorf("marker = %q (stored=%v, err=%v), want true", got, ok, err)
	}
}
