// Package researchfxflow_test checks the research FX settings saved through
// opsettings.Service. It lives in its own directory to keep
// internal/service/opsettings within the linterly directory budget.
package researchfxflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/researchfxsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// The FX record's switch is off and its pair is USD_JPY while nothing is stored;
// both keys save in the form researchfxsettings.Load reads, and reset removes them.
func TestResearchFX_DefaultSaveReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	v, err := svc.Get(ctx, researchfxsettings.KeyEnabled)
	if err != nil || v.Current != "0" || v.Default != "0" || v.Overridden {
		t.Fatalf("enabled default = %+v, %v, want 0 (off)", v, err)
	}
	if v, err = svc.Get(ctx, researchfxsettings.KeySymbols); err != nil || v.Current != "USD_JPY" || v.Default != "USD_JPY" || v.Overridden {
		t.Fatalf("symbols default = %+v, %v, want USD_JPY", v, err)
	}
	if err := svc.Save(ctx, researchfxsettings.KeyEnabled, " 1 "); err != nil {
		t.Fatalf("Save enabled: %v", err)
	}
	if err := svc.Save(ctx, researchfxsettings.KeySymbols, " USD_JPY , EUR_USD "); err != nil {
		t.Fatalf("Save symbols: %v", err)
	}
	if store.rows[researchfxsettings.KeyEnabled] != "1" || store.rows[researchfxsettings.KeySymbols] != `"USD_JPY,EUR_USD"` {
		t.Fatalf("stored = %v", store.rows)
	}
	got, err := researchfxsettings.Load(ctx, storeReader{store})
	if err != nil || !got.Enabled || len(got.Symbols) != 2 {
		t.Fatalf("Load of what was saved = %+v, %v", got, err)
	}
	if v, _ = svc.Get(ctx, researchfxsettings.KeySymbols); v.Current != "USD_JPY,EUR_USD" || !v.Overridden {
		t.Fatalf("symbols after save = %+v", v)
	}
	for _, key := range researchfxsettings.Keys() {
		if err := svc.Reset(ctx, key); err != nil {
			t.Fatalf("Reset %s: %v", key, err)
		}
		if _, ok := store.rows[key]; ok {
			t.Fatalf("Reset left %s", key)
		}
	}
}

func TestResearchFX_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	for key, raws := range map[string][]string{
		researchfxsettings.KeyEnabled: {"2", "-1", "yes", "0.5", "true"},
		researchfxsettings.KeySymbols: {"usd_jpy", "USDJPY", "USD_JPY,USD_JPY", "USD_JPY,", "USD_JPY,EUR_USD,EUR_JPY,GBP_JPY,AUD_JPY,NZD_JPY", "USD_JPY EUR_USD"},
	} {
		for _, raw := range raws {
			var inv *opsettings.InvalidValueError
			if err := svc.Save(ctx, key, raw); !errors.As(err, &inv) {
				t.Errorf("Save(%s, %q) = %v, want *InvalidValueError", key, raw, err)
			}
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("an invalid value was stored: %v", store.rows)
	}
}

type fakeStore struct{ rows map[string]string }

func (f *fakeStore) Get(_ context.Context, key string) (string, bool, error) {
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

func newService(store *fakeStore) *opsettings.Service {
	return opsettings.New(store, config.StrategyConfig{}, "/data/logs")
}

type storeReader struct{ s *fakeStore }

func (r storeReader) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := r.s.rows[key]
	return v, ok, nil
}
