// This file checks the research index settings saved through opsettings.Service.
// It lives here, not in internal/service/opsettings, to keep that directory
// within the linterly directory budget.
package researchrecordsettings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/researchindexsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// The index record's switch is off and its codes are 101,151 while nothing is stored;
// both keys save in the form researchindexsettings.Load reads, and reset removes them.
func TestResearchIndex_DefaultSaveReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	v, err := svc.Get(ctx, researchindexsettings.KeyEnabled)
	if err != nil || v.Current != "0" || v.Default != "0" || v.Overridden {
		t.Fatalf("enabled default = %+v, %v, want 0 (off)", v, err)
	}
	if v, err = svc.Get(ctx, researchindexsettings.KeySymbols); err != nil || v.Current != "101,151" || v.Default != "101,151" || v.Overridden {
		t.Fatalf("symbols default = %+v, %v, want 101,151", v, err)
	}
	if err := svc.Save(ctx, researchindexsettings.KeyEnabled, " 1 "); err != nil {
		t.Fatalf("Save enabled: %v", err)
	}
	if err := svc.Save(ctx, researchindexsettings.KeySymbols, " 101 , 102 "); err != nil {
		t.Fatalf("Save symbols: %v", err)
	}
	if store.rows[researchindexsettings.KeyEnabled] != "1" || store.rows[researchindexsettings.KeySymbols] != `"101,102"` {
		t.Fatalf("stored = %v", store.rows)
	}
	got, err := researchindexsettings.Load(ctx, storeReader{store})
	if err != nil || !got.Enabled || len(got.Symbols) != 2 {
		t.Fatalf("Load of what was saved = %+v, %v", got, err)
	}
	if v, _ = svc.Get(ctx, researchindexsettings.KeySymbols); v.Current != "101,102" || !v.Overridden {
		t.Fatalf("symbols after save = %+v", v)
	}
	for _, key := range researchindexsettings.Keys() {
		if err := svc.Reset(ctx, key); err != nil {
			t.Fatalf("Reset %s: %v", key, err)
		}
		if _, ok := store.rows[key]; ok {
			t.Fatalf("Reset left %s", key)
		}
	}
}

func TestResearchIndex_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	for key, raws := range map[string][]string{
		researchindexsettings.KeyEnabled: {"2", "-1", "yes", "0.5", "true"},
		researchindexsettings.KeySymbols: {"1 01", "101,101", "101,", "101,102,103,104,105,106", "101 102", "USD_JPY"},
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
