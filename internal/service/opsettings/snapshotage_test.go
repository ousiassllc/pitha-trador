package opsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/snapshotagesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestSnapshotMaxAge_DefaultSaveReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	v, err := svc.Get(ctx, snapshotagesettings.Key)
	if err != nil || v.Current != "20" || v.Default != "20" || v.Overridden {
		t.Fatalf("Get default = %+v, %v, want 20 not overridden (issue #754)", v, err)
	}
	if err := svc.Save(ctx, snapshotagesettings.Key, " 45 "); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.rows[snapshotagesettings.Key]; got != "45" {
		t.Fatalf("stored = %q, want 45", got)
	}
	if v, _ = svc.Get(ctx, snapshotagesettings.Key); v.Current != "45" || !v.Overridden || v.Default != "20" {
		t.Fatalf("Get after save = %+v", v)
	}
	if err := svc.Reset(ctx, snapshotagesettings.Key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[snapshotagesettings.Key]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestSnapshotMaxAge_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)
	for name, raw := range map[string]string{
		"not an integer": "2.5", "text": "abc", "zero": "0", "negative": "-1", "above the maximum": "601",
	} {
		err := svc.Save(ctx, snapshotagesettings.Key, raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: Save(%q) error = %v, want *InvalidValueError", name, raw, err)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}
}
