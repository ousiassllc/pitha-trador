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

// Issue #836: a failed Paper seed must not let the bucket seed run on the
// config/risk.yaml capital (30,000,000 yen here) - it would store that as the
// stock budget and the marker would keep the next start from correcting it.
func TestSeed_PaperSeedFailureLeavesBucketRowsAndMarkerUnwritten(t *testing.T) {
	st := &store{rows: map[string]string{}, setErr: errors.New("disk full"), failKey: papersettings.KeyInitialCapitalJPY}
	paperrisk.Seed(context.Background(), st, fakeLimits{limits: config.RiskLimits{InitialCapital: 30_000_000}}, time.Now())
	for _, key := range append(bucketsettings.Keys(), bucketsettings.SeededMarkerKey, papersettings.SeededMarkerKey) {
		if v, ok := st.rows[key]; ok {
			t.Errorf("%s = %q was stored although the Paper seed failed", key, v)
		}
	}
}

func TestSeed_NextStartSeedsBothAfterAPaperSeedFailure(t *testing.T) {
	ctx := context.Background()
	st := &store{rows: map[string]string{}, setErr: errors.New("disk full"), failKey: papersettings.KeyInitialCapitalJPY}
	paperrisk.Seed(ctx, st, fakeLimits{limits: config.RiskLimits{InitialCapital: 30_000_000}}, time.Now())

	// The next start: the store works again and the capital in force is the Paper default.
	st.setErr = nil
	paperrisk.Seed(ctx, st, fakeLimits{limits: config.RiskLimits{InitialCapital: 500_000}}, time.Now())
	want := map[string]string{
		papersettings.KeyInitialCapitalJPY:       "500000",
		papersettings.KeyMaxPositionPerSymbolPct: "100",
		papersettings.KeyMaxTotalExposurePct:     "100",
		papersettings.KeyMaxTradeLossPct:         "1",
		papersettings.SeededMarkerKey:            "true",
		bucketsettings.KeyStockBudgetJPY:         "500000",
		bucketsettings.KeyBondETFBudgetJPY:       "0",
		bucketsettings.SeededMarkerKey:           "true",
	}
	if len(st.rows) != len(want) {
		t.Errorf("rows = %v, want %v", st.rows, want)
	}
	for k, v := range want {
		if st.rows[k] != v {
			t.Errorf("%s = %q, want %q", k, st.rows[k], v)
		}
	}
}
