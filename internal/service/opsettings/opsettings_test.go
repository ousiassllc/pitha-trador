package opsettings_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

type fakeStore struct {
	rows   map[string]string
	getErr error
}

func (f *fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.rows[key]
	return v, ok, nil
}

func (f *fakeStore) Set(_ context.Context, key, value string, _ time.Time) error {
	f.rows[key] = value
	return nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	delete(f.rows, key)
	return nil
}

func validStrategy() config.StrategyConfig {
	dir := config.PolicyDirectionThresholds{MinProbability: 0.7, MinEntryQuality: "good", MinContinuationProbability: 0.6, MaxToxicFlow: 0.3, MaxLiquidityStressed: 0.3}
	return config.StrategyConfig{
		Policy: config.PolicyConfig{Long: dir, Short: dir},
		FastScreener: config.FastScreenerConfig{
			MinPrice: 100, MaxPrice: 5000, MinTurnover5mJPY: 1e7, MaxSpreadBps: 20, MinVolumeRatio: 2,
			MinAbsReturn5mPct: 0.5, MinRealizedVolatility: 0.1, TopN: 10,
			Weights: config.FastScreenerWeights{VolumeRatio: 1, AbsReturn5m: 1, BreakoutStrength: 1, OrderbookImbalance: 1, VolatilityExpansion: 1},
		},
	}
}

func newService(store *fakeStore) *opsettings.Service {
	return opsettings.New(store, validStrategy(), "/data/logs")
}

func TestKeys_CoverEveryManagedKeyOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range opsettings.Keys() {
		if seen[k] {
			t.Errorf("duplicate key %q", k)
		}
		seen[k] = true
	}
	if want := 2 + len(config.PolicySettingKeys()) + len(config.FastScreenerSettingKeys()) + len(config.BrokerSettingKeys()); len(seen) != want {
		t.Errorf("len(Keys) = %d, want %d", len(seen), want)
	}
}

func TestGet_UsesYamlUntilOverridden(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	ctx := context.Background()

	v, err := svc.Get(ctx, "policy.long.min_probability")
	if err != nil || v.Current != "0.7" || v.Default != "0.7" || v.Overridden {
		t.Fatalf("yaml value = %+v, %v; want current/default 0.7, not overridden", v, err)
	}
	if v, _ := svc.Get(ctx, "screener.top_n"); v.Current != "10" {
		t.Errorf("screener.top_n current = %q, want 10", v.Current)
	}

	if err := svc.Save(ctx, "policy.long.min_probability", " 0.75 "); err != nil {
		t.Fatalf("Save: %v", err)
	}
	v, _ = svc.Get(ctx, "policy.long.min_probability")
	if v.Current != "0.75" || v.Default != "0.7" || !v.Overridden {
		t.Fatalf("after Save = %+v, want current 0.75 (default 0.7, overridden)", v)
	}
	if store.rows["policy.long.min_probability"] != "0.75" {
		t.Errorf("stored row = %q, want the JSON number 0.75", store.rows["policy.long.min_probability"])
	}

	if err := svc.Reset(ctx, "policy.long.min_probability"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if v, _ = svc.Get(ctx, "policy.long.min_probability"); v.Current != "0.7" || v.Overridden {
		t.Fatalf("after Reset = %+v, want the yaml value back", v)
	}
}

func TestSave_EntryQualityAndTopNAreStoredAsJSONScalars(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	ctx := context.Background()

	if err := svc.Save(ctx, "policy.short.min_entry_quality", "strong"); err != nil {
		t.Fatalf("Save entry quality: %v", err)
	}
	if err := svc.Save(ctx, "screener.top_n", "25"); err != nil {
		t.Fatalf("Save top_n: %v", err)
	}
	if store.rows["policy.short.min_entry_quality"] != `"strong"` || store.rows["screener.top_n"] != "25" {
		t.Errorf("rows = %v", store.rows)
	}
}

func TestSave_RejectsInvalidValuesAndLeavesTheStoreUntouched(t *testing.T) {
	cases := []struct{ key, value string }{
		{"policy.long.min_probability", "0"},
		{"policy.long.min_probability", "1.5"},
		{"policy.long.min_probability", "abc"},
		{"policy.long.min_probability", "NaN"},
		{"policy.short.min_entry_quality", "stong"},
		{"screener.top_n", "0"},
		{"screener.top_n", "2.5"},
		{"screener.max_price", "50"}, // below yaml min_price=100
		{"screener.min_price", "-1"}, // not > 0
		{"screener.weights.volume_ratio", "-0.1"},
		{config.KeyBackupDir, "relative/dir"},
		{config.KeyLogDir, "logs\x00x"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			store := &fakeStore{rows: map[string]string{}}
			err := newService(store).Save(context.Background(), tc.key, tc.value)
			var invalid *opsettings.InvalidValueError
			if !errors.As(err, &invalid) {
				t.Fatalf("Save error = %v, want *InvalidValueError", err)
			}
			if len(store.rows) != 0 {
				t.Errorf("store written despite invalid value: %v", store.rows)
			}
		})
	}
}

func TestSave_ValidatesAgainstEffectiveThresholds(t *testing.T) {
	// The stored min_price=400 makes max_price=300 invalid even though 300
	// is above the yaml min_price.
	store := &fakeStore{rows: map[string]string{"screener.min_price": "400"}}
	err := newService(store).Save(context.Background(), "screener.max_price", "300")
	var invalid *opsettings.InvalidValueError
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Reason, "screener.max_price") {
		t.Fatalf("Save error = %v, want an InvalidValueError naming screener.max_price", err)
	}
}

func TestSave_BlankUnknownAndStoreErrors(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	ctx := context.Background()

	if err := svc.Save(ctx, "policy.long.min_probability", "  "); !errors.Is(err, opsettings.ErrEmptyValue) {
		t.Errorf("blank value error = %v, want ErrEmptyValue", err)
	}
	if err := svc.Save(ctx, "risk.max_position_size", "1"); !errors.Is(err, opsettings.ErrUnknownKey) {
		t.Errorf("unknown key error = %v, want ErrUnknownKey", err)
	}
	if err := svc.Reset(ctx, "jev.api_key"); !errors.Is(err, opsettings.ErrUnknownKey) {
		t.Errorf("Reset unknown key error = %v, want ErrUnknownKey", err)
	}
	if _, err := svc.Get(ctx, "nope"); !errors.Is(err, opsettings.ErrUnknownKey) {
		t.Errorf("Get unknown key error = %v, want ErrUnknownKey", err)
	}

	store.getErr = errors.New("db locked")
	if err := svc.Save(ctx, "screener.top_n", "5"); err == nil || errors.As(err, new(*opsettings.InvalidValueError)) {
		t.Errorf("Save with an unreadable store = %v, want a plain error", err)
	}
}

func TestPathSettings_DefaultsAndBackupDirWarning(t *testing.T) {
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	ctx := context.Background()

	backup, _ := svc.Get(ctx, config.KeyBackupDir)
	if backup.Current != "" || backup.Default != "" || backup.Overridden || backup.Warning != "" {
		t.Errorf("unset backup dir = %+v, want empty (disabled) with no warning", backup)
	}
	logDir, _ := svc.Get(ctx, config.KeyLogDir)
	if logDir.Current != "" || logDir.Default != "/data/logs" {
		t.Errorf("unset log dir = %+v, want empty current and the default dir", logDir)
	}

	dest := t.TempDir()
	if err := svc.Save(ctx, config.KeyBackupDir, dest); err != nil {
		t.Fatalf("Save backup dir: %v", err)
	}
	if v, _ := svc.Get(ctx, config.KeyBackupDir); v.Current != dest || !v.Overridden || v.Warning != "" {
		t.Errorf("available backup dir = %+v, want no warning", v)
	}

	gone := filepath.Join(dest, "unmounted")
	if err := svc.Save(ctx, config.KeyBackupDir, gone); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if v, _ := svc.Get(ctx, config.KeyBackupDir); v.Warning == "" {
		t.Error("a missing backup dir must carry a warning")
	}
	if _, err := os.Stat(gone); err == nil {
		t.Error("Save created the backup destination")
	}
}
