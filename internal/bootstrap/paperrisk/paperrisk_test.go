package paperrisk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperrisk"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/bucketsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

type store struct {
	rows   map[string]string
	getErr error
	setErr error
	// failKey, when set, makes the write of that one key fail with setErr.
	failKey string
}

func (s *store) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, s.getErr
}

func (s *store) SetIfAbsent(_ context.Context, key, value string, _ time.Time) (bool, error) {
	if s.setErr != nil && (s.failKey == "" || s.failKey == key) {
		return false, s.setErr
	}
	if _, ok := s.rows[key]; ok {
		return false, nil
	}
	s.rows[key] = value
	return true, nil
}

func TestCapitalSource_ReadsTheSavedCapitalOnEveryCall(t *testing.T) {
	ctx := context.Background()
	st := &store{rows: map[string]string{}}
	source := paperrisk.CapitalSource(st, config.RiskLimits{InitialCapital: 30_000_000})
	if got, err := source(ctx); err != nil || got != 30_000_000 {
		t.Fatalf("before saving = %v, %v, want risk.yaml's 30000000", got, err)
	}
	st.rows[papersettings.KeyInitialCapitalJPY] = "500000"
	if got, err := source(ctx); err != nil || got != 500_000 {
		t.Fatalf("after saving = %v, %v, want 500000 without a restart", got, err)
	}
	st.getErr = errors.New("db down")
	if _, err := source(ctx); err == nil {
		t.Fatal("a store failure was swallowed")
	}
}

type fakeLimits struct {
	limits config.RiskLimits
	err    error
}

func (f fakeLimits) EffectiveLimits(context.Context) (config.RiskLimits, error) {
	return f.limits, f.err
}

func TestCapitalNow_ZeroWhenUnreadable(t *testing.T) {
	ctx := context.Background()
	if got := paperrisk.CapitalNow(fakeLimits{limits: config.RiskLimits{InitialCapital: 500_000}})(ctx); got != 500_000 {
		t.Errorf("CapitalNow = %v, want 500000", got)
	}
	if got := paperrisk.CapitalNow(fakeLimits{err: errors.New("down")})(ctx); got != 0 {
		t.Errorf("CapitalNow on a read failure = %v, want 0", got)
	}
}

func TestSeed_FailureDoesNotPanicAndStoresNothing(t *testing.T) {
	st := &store{rows: map[string]string{}, setErr: errors.New("disk full")}
	paperrisk.Seed(context.Background(), st, fakeLimits{limits: config.RiskLimits{InitialCapital: 500_000}}, time.Now())
	if len(st.rows) != 0 {
		t.Errorf("rows = %v, want none", st.rows)
	}
}

func TestSeed_StockBucketGetsTheCapitalInForceAndBondETFZero(t *testing.T) {
	st := &store{rows: map[string]string{}}
	paperrisk.Seed(context.Background(), st, fakeLimits{limits: config.RiskLimits{InitialCapital: 500_000}}, time.Now())
	if st.rows[bucketsettings.KeyStockBudgetJPY] != "500000" || st.rows[bucketsettings.KeyBondETFBudgetJPY] != "0" || st.rows[bucketsettings.SeededMarkerKey] != "true" {
		t.Errorf("rows = %v, want stock 500000 / bond_etf 0 / marker", st.rows)
	}
}

func TestSeed_UnreadableCapitalLeavesTheBucketsUnseeded(t *testing.T) {
	st := &store{rows: map[string]string{}}
	paperrisk.Seed(context.Background(), st, fakeLimits{err: errors.New("down")}, time.Now())
	for _, key := range append(bucketsettings.Keys(), bucketsettings.SeededMarkerKey) {
		if _, ok := st.rows[key]; ok {
			t.Errorf("%s was stored although the capital could not be read", key)
		}
	}
}
