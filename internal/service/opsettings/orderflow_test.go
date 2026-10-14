package opsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/orderflowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

func TestOrderFlowWindows_DefaultsAndSave(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	v, err := svc.Get(ctx, orderflowsettings.KeyMid)
	if err != nil || v.Current != "30" || v.Default != "30" || v.Overridden {
		t.Fatalf("Get default = %+v, %v, want 30 not overridden", v, err)
	}

	if err := svc.Save(ctx, orderflowsettings.KeyShort, " 5 "); err != nil {
		t.Fatalf("Save short: %v", err)
	}
	if got := store.rows[orderflowsettings.KeyShort]; got != "5" {
		t.Fatalf("stored short = %q, want 5", got)
	}
	v, _ = svc.Get(ctx, orderflowsettings.KeyShort)
	if v.Current != "5" || !v.Overridden || v.Default != "10" {
		t.Fatalf("Get after save = %+v", v)
	}

	if err := svc.Reset(ctx, orderflowsettings.KeyShort); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[orderflowsettings.KeyShort]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestOrderFlowWindows_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for name, tc := range map[string]struct{ key, raw string }{
		"not an integer":       {orderflowsettings.KeyShort, "2.5"},
		"text":                 {orderflowsettings.KeyShort, "abc"},
		"zero":                 {orderflowsettings.KeyShort, "0"},
		"negative":             {orderflowsettings.KeyShort, "-1"},
		"above the maximum":    {orderflowsettings.KeyLong, "601"},
		"short >= default mid": {orderflowsettings.KeyShort, "30"},
		"mid <= default short": {orderflowsettings.KeyMid, "10"},
		"mid >= default long":  {orderflowsettings.KeyMid, "60"},
		"long <= default mid":  {orderflowsettings.KeyLong, "30"},
	} {
		err := svc.Save(ctx, tc.key, tc.raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: Save(%s, %q) error = %v, want *InvalidValueError", name, tc.key, tc.raw, err)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}

	// The ordering is checked against the other stored windows, so widening
	// all three in the right order works while the wrong order is refused.
	if err := svc.Save(ctx, orderflowsettings.KeyLong, "120"); err != nil {
		t.Fatalf("Save long 120: %v", err)
	}
	if err := svc.Save(ctx, orderflowsettings.KeyMid, "90"); err != nil {
		t.Fatalf("Save mid 90: %v", err)
	}
	if err := svc.Save(ctx, orderflowsettings.KeyShort, "80"); err != nil {
		t.Fatalf("Save short 80: %v", err)
	}
	if err := svc.Save(ctx, orderflowsettings.KeyShort, "90"); err == nil {
		t.Fatal("Save short == stored mid: want an error")
	}
}

// issue #753: the daily entry-halt thresholds are plain per-key numbers,
// disabled (0) by default.
func TestDailyGuardThresholds_DefaultSaveAndReset(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for _, key := range dailyguardsettings.Keys() {
		if v, err := svc.Get(ctx, key); err != nil || v.Current != "0" || v.Default != "0" || v.Overridden {
			t.Fatalf("Get(%s) default = %+v, %v, want 0 not overridden", key, v, err)
		}
	}

	if err := svc.Save(ctx, dailyguardsettings.KeyLossLimitJPY, " 50000 "); err != nil {
		t.Fatalf("Save loss limit: %v", err)
	}
	if got := store.rows[dailyguardsettings.KeyLossLimitJPY]; got != "50000" {
		t.Fatalf("stored loss limit = %q, want 50000", got)
	}
	if err := svc.Save(ctx, dailyguardsettings.KeyMaxTrades, "6"); err != nil {
		t.Fatalf("Save max trades: %v", err)
	}
	if got := store.rows[dailyguardsettings.KeyMaxTrades]; got != "6" {
		t.Fatalf("stored max trades = %q, want 6 (an integer)", got)
	}
	limits, err := dailyguardsettings.Load(ctx, store)
	if err != nil || limits.LossLimitJPY != 50000 || limits.MaxTrades != 6 {
		t.Fatalf("Load after Save = %+v, %v", limits, err)
	}

	if err := svc.Reset(ctx, dailyguardsettings.KeyLossLimitJPY); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := store.rows[dailyguardsettings.KeyLossLimitJPY]; ok {
		t.Fatal("Reset left the row")
	}
}

func TestDailyGuardThresholds_SaveRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{rows: map[string]string{}}
	svc := newService(store)

	for name, tc := range map[string]struct{ key, raw string }{
		"negative loss limit":     {dailyguardsettings.KeyLossLimitJPY, "-1"},
		"text":                    {dailyguardsettings.KeyLossLimitJPY, "abc"},
		"giveback above 100":      {dailyguardsettings.KeyProfitLockGivebackPct, "101"},
		"fractional trade count":  {dailyguardsettings.KeyMaxTrades, "2.5"},
		"negative trade count":    {dailyguardsettings.KeyMaxTrades, "-1"},
		"negative profit trigger": {dailyguardsettings.KeyProfitLockTriggerJPY, "-5"},
	} {
		err := svc.Save(ctx, tc.key, tc.raw)
		var invalid *opsettings.InvalidValueError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: Save(%s, %q) error = %v, want *InvalidValueError", name, tc.key, tc.raw, err)
		}
	}
	if err := svc.Save(ctx, dailyguardsettings.KeyMaxTrades, ""); !errors.Is(err, opsettings.ErrEmptyValue) {
		t.Errorf("blank Save error = %v, want ErrEmptyValue", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("rejected values were stored: %v", store.rows)
	}
}
