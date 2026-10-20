// Package bootstrapflow_test checks the Paper bootstrap switch (issue #798)
// saved through opsettings.Service. It lives in its own directory to keep
// internal/service/opsettings within the linterly directory budget.
package bootstrapflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/paperbootstrapsettings"
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

func newService(store *fakeStore) *opsettings.Service {
	return opsettings.New(store, config.StrategyConfig{}, "/data/logs")
}

// issue #798: the Paper-only bootstrap switch is off by default and a Paper
// service stores and resets it like the other integer switches.
func TestPaperBootstrap_DefaultAndSaveInPaper(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store).WithTradingMode(metalabelsettings.ModePaper)

	v, err := svc.Get(ctx, paperbootstrapsettings.Key)
	if err != nil || v.Current != "0" || v.Default != "0" || v.Overridden || v.Warning != "" {
		t.Fatalf("Get = %+v, %v, want 0 (off) not overridden", v, err)
	}
	if err := svc.Save(ctx, paperbootstrapsettings.Key, " 1 "); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if got := store.rows[paperbootstrapsettings.Key]; got != "1" {
		t.Fatalf("stored = %q, want 1 (a JSON integer)", got)
	}
	if v, _ := svc.Get(ctx, paperbootstrapsettings.Key); v.Current != "1" || !v.Overridden || v.Warning != "" {
		t.Fatalf("Get after save = %+v", v)
	}
	if on, err := (paperbootstrapsettings.Source{Store: store}).PaperBootstrapEnabled(ctx); err != nil || !on {
		t.Fatalf("PaperBootstrapEnabled = %v, %v, want on after saving 1", on, err)
	}
	if err := svc.Reset(ctx, paperbootstrapsettings.Key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if on, err := (paperbootstrapsettings.Source{Store: store}).PaperBootstrapEnabled(ctx); err != nil || on {
		t.Fatalf("PaperBootstrapEnabled = %v, %v, want off after Reset", on, err)
	}
}

func TestPaperBootstrap_SaveRejectsInvalid(t *testing.T) {
	svc := newService(&fakeStore{rows: map[string]string{}}).WithTradingMode(metalabelsettings.ModePaper)
	for _, raw := range []string{"2", "-1", "0.5", "on", "NaN"} {
		var invalid *opsettings.InvalidValueError
		if err := svc.Save(context.Background(), paperbootstrapsettings.Key, raw); !errors.As(err, &invalid) {
			t.Errorf("Save(%q) = %v, want *InvalidValueError", raw, err)
		}
	}
}

// HARD RULE (issue #798): outside Paper the switch cannot be turned on, and a
// stored on is shown as inactive. Turning it off stays possible.
func TestPaperBootstrap_RefusedOutsidePaper(t *testing.T) {
	ctx := context.Background()
	for name, mode := range map[string]metalabelsettings.Mode{"Live": metalabelsettings.ModeLive, "mode never set": ""} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{rows: map[string]string{}}
			svc := newService(store)
			if mode != "" {
				svc = svc.WithTradingMode(mode)
			}

			var invalid *opsettings.InvalidValueError
			if err := svc.Save(ctx, paperbootstrapsettings.Key, "1"); !errors.As(err, &invalid) {
				t.Fatalf("Save 1 = %v, want *InvalidValueError", err)
			}
			if _, ok := store.rows[paperbootstrapsettings.Key]; ok {
				t.Fatal("the refused value was stored")
			}
			if err := svc.Save(ctx, paperbootstrapsettings.Key, "0"); err != nil {
				t.Fatalf("Save 0 (turning it off) = %v, want ok", err)
			}

			store.rows[paperbootstrapsettings.Key] = "1" // e.g. a row written while the build was Paper
			v, err := svc.Get(ctx, paperbootstrapsettings.Key)
			if err != nil || v.Current != "1" || v.Warning == "" {
				t.Fatalf("Get = %+v, %v, want the stored 1 with a warning that it is inactive", v, err)
			}
		})
	}
}

func TestPaperBootstrap_SourceIgnoresDamagedRow(t *testing.T) {
	store := &fakeStore{rows: map[string]string{paperbootstrapsettings.Key: `"yes"`}}
	if on, err := (paperbootstrapsettings.Source{Store: store}).PaperBootstrapEnabled(context.Background()); err != nil || on {
		t.Fatalf("PaperBootstrapEnabled = %v, %v, want off for a damaged row", on, err)
	}
	store.getErr = errors.New("db down")
	if _, err := (paperbootstrapsettings.Source{Store: store}).PaperBootstrapEnabled(context.Background()); err == nil {
		t.Fatal("a read failure must be an error")
	}
}
