package papersettings_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
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

func TestSeedDefaults_FreshDatabaseStoresThePaperInitialValuesAndMarker(t *testing.T) {
	store := &seedStore{rows: map[string]string{}}
	if err := papersettings.SeedDefaults(context.Background(), store, seedNow); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	want := map[string]string{
		papersettings.KeyInitialCapitalJPY:       "500000",
		papersettings.KeyMaxPositionPerSymbolPct: "100",
		papersettings.KeyMaxTotalExposurePct:     "100",
		papersettings.KeyMaxTradeLossPct:         "1",
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

// Issue #837: a value the Settings screen or the MCP stores after the seed
// decided "nothing stored" - between its read and its write - survives the seed.
func TestSeedDefaults_ConcurrentSaveIsNotOverwrittenOnARealDatabase(t *testing.T) {
	ctx := context.Background()
	store, repo := newRacingStore(t)
	keys := papersettings.Keys()
	last := keys[len(keys)-1]
	saved := false
	store.onGet = func(key string) {
		// The seed's last row read (nothing stored yet) is followed by the save.
		if key != last || saved {
			return
		}
		saved = true
		if err := repo.Set(ctx, papersettings.KeyInitialCapitalJPY, "1000000", seedNow.Add(time.Second)); err != nil {
			t.Errorf("concurrent save: %v", err)
		}
	}
	if err := papersettings.SeedDefaults(ctx, store, seedNow); err != nil {
		t.Fatal(err)
	}
	if !saved {
		t.Fatal("the concurrent save never ran")
	}
	if got, ok, err := repo.Get(ctx, papersettings.KeyInitialCapitalJPY); err != nil || !ok || got != "1000000" {
		t.Errorf("capital = %q (stored=%v, err=%v), want the saved 1000000 kept", got, ok, err)
	}
	if got, ok, err := repo.Get(ctx, papersettings.SeededMarkerKey); err != nil || !ok || got != "true" {
		t.Errorf("marker = %q (stored=%v, err=%v), want true", got, ok, err)
	}
}
