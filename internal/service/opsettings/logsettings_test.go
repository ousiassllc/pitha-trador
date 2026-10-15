package opsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestLogLevel_DefaultSaveReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	v, err := svc.Get(ctx, config.KeyLogLevel)
	if err != nil || v.Current != "info" || v.Default != "info" || v.Overridden {
		t.Fatalf("Get default = %+v, %v, want info not overridden (issue #772)", v, err)
	}
	if err := svc.Save(ctx, config.KeyLogLevel, " WARN "); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.rows[config.KeyLogLevel]; got != `"warn"` {
		t.Fatalf("stored = %q, want \"warn\"", got)
	}
	if v, _ = svc.Get(ctx, config.KeyLogLevel); v.Current != "warn" || !v.Overridden {
		t.Fatalf("Get after save = %+v", v)
	}
	if err := svc.Reset(ctx, config.KeyLogLevel); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[config.KeyLogLevel]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestLogRetentionDays_DefaultSaveReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	v, err := svc.Get(ctx, config.KeyLogRetentionDays)
	if err != nil || v.Current != "30" || v.Default != "30" || v.Overridden {
		t.Fatalf("Get default = %+v, %v, want 30 not overridden", v, err)
	}
	if err := svc.Save(ctx, config.KeyLogRetentionDays, " 7 "); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.rows[config.KeyLogRetentionDays]; got != "7" {
		t.Fatalf("stored = %q, want 7", got)
	}
	if v, _ = svc.Get(ctx, config.KeyLogRetentionDays); v.Current != "7" || !v.Overridden {
		t.Fatalf("Get after save = %+v", v)
	}
	if err := svc.Reset(ctx, config.KeyLogRetentionDays); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[config.KeyLogRetentionDays]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestLogSettings_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	for key, raws := range map[string][]string{
		config.KeyLogLevel:         {"trace", "3", "warning"},
		config.KeyLogRetentionDays: {"2.5", "abc", "0", "-1", "3651"},
	} {
		for _, raw := range raws {
			err := svc.Save(ctx, key, raw)
			var invalid *opsettings.InvalidValueError
			if !errors.As(err, &invalid) {
				t.Errorf("Save(%s, %q) error = %v, want *InvalidValueError", key, raw, err)
			}
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}
}
