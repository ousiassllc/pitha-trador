package dailyguardseed_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/dailyguardseed"
	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

type store struct {
	rows   map[string]string
	setErr error
}

func (s *store) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.rows[key]
	return v, ok, nil
}

func (s *store) Set(_ context.Context, key, value string, _ time.Time) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.rows[key] = value
	return nil
}

func (s *store) Delete(_ context.Context, key string) error { delete(s.rows, key); return nil }

func TestRun_SeedsFreshDatabaseOnceAndKeepsDeletedRowsDeleted(t *testing.T) {
	ctx := context.Background()
	st := &store{rows: map[string]string{}}

	dailyguardseed.Run(ctx, st, time.Now())
	limits, err := dailyguardsettings.Load(ctx, st)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := dailyguardsettings.Limits{LossLimitJPY: 5000, ProfitLockTriggerJPY: 2500, ProfitLockGivebackPct: 50, MaxTrades: 20}
	if limits != want {
		t.Fatalf("seeded Limits = %+v, want %+v", limits, want)
	}

	// The owner removes a row; a later start does not bring it back.
	_ = st.Delete(ctx, dailyguardsettings.KeyMaxTrades)
	dailyguardseed.Run(ctx, st, time.Now())
	if _, ok := st.rows[dailyguardsettings.KeyMaxTrades]; ok {
		t.Fatal("a second Run re-seeded a deleted row")
	}
}

func TestRun_StoreFailureDoesNotPanicOrSeed(t *testing.T) {
	st := &store{rows: map[string]string{}, setErr: errors.New("db down")}
	dailyguardseed.Run(context.Background(), st, time.Now())
	if len(st.rows) != 0 {
		t.Fatalf("rows = %v, want none", st.rows)
	}
}
