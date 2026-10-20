package paperrisk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperrisk"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/papersettings"
)

type store struct {
	rows   map[string]string
	getErr error
	setErr error
}

func (s *store) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, s.getErr
}

func (s *store) Set(_ context.Context, key, value string, _ time.Time) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.rows[key] = value
	return nil
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
	paperrisk.Seed(context.Background(), st, time.Now())
	if len(st.rows) != 0 {
		t.Errorf("rows = %v, want none", st.rows)
	}
}
