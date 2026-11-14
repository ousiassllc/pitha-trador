package trial

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
)

// memStore is an empty runtime_settings (no stop records).
type memStore struct{ values map[string]string }

func (m *memStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memStore) Set(_ context.Context, key, value string, _ time.Time) error {
	m.values[key] = value
	return nil
}

// spentResults reports a fixed stored cost.
type spentResults struct{ total, arm float64 }

func (spentResults) Insert(context.Context, jevtrial.Decision) (jevtrial.Decision, error) {
	return jevtrial.Decision{}, nil
}

func (spentResults) InsertSkip(context.Context, jevtrial.Skip) error { return nil }

func (r spentResults) SpentBetween(context.Context, time.Time, time.Time) (judgement.TrialSpend, error) {
	return judgement.TrialSpend{CountedCostUSD: r.total}, nil
}

func (r spentResults) SpentByArmBetween(context.Context, string, time.Time, time.Time) (judgement.TrialSpend, error) {
	return judgement.TrialSpend{CountedCostUSD: r.arm}, nil
}

func newReserveTrial(results Results) (*Trial, *memStore) {
	store := &memStore{values: map[string]string{}}
	t := New(results, store, func(jevtrialsettings.Arm) Caller { return nil }, metalabelsettings.ModePaper, time.Now)
	return t, store
}

// The in-flight calls are held against the overall cap: with room for two
// reservations, a third call is refused (without a stop record) until one of
// them is released.
func TestMayCallReservesInFlightCallsAgainstTheOverallCap(t *testing.T) {
	tr, store := newReserveTrial(spentResults{})
	cfg := jevtrialsettings.Settings{CapUSD: 0.0025}
	now := time.Now()
	const r = 0.001
	mayCall := func(arm string) *reservation {
		res, _, err := tr.mayCall(context.Background(), cfg, jevtrialsettings.Arm{ID: arm}, now, r)
		if err != nil {
			t.Fatalf("mayCall: %v", err)
		}
		return res
	}
	first, second := mayCall("a1"), mayCall("a2")
	if first == nil || second == nil {
		t.Fatalf("the first two calls fit under the cap: %v %v", first, second)
	}
	if third := mayCall("a3"); third != nil {
		t.Fatal("a third call was allowed: 3 reservations exceed the cap")
	}
	if len(store.values) != 0 {
		t.Errorf("a refusal caused only by reservations recorded a stop: %v", store.values)
	}
	if total, _ := tr.reserved("a3"); total != 2*r {
		t.Errorf("reserved = %v, want %v: a refused call must give its reservation back", total, 2*r)
	}
	first.release()
	first.release() // a second release is a no-op
	fourth := mayCall("a4")
	if fourth == nil {
		t.Fatal("the call was refused although a reservation was released")
	}
	second.release()
	fourth.release()
	if total, byArm := tr.reserved("a1"); total != 0 || byArm != 0 {
		t.Errorf("reserved after releasing everything = %v %v, want 0", total, byArm)
	}
}

// The arm's own cap holds the reservations of that arm only; the stored cost
// counts with them.
func TestMayCallReservesInFlightCallsAgainstTheArmCap(t *testing.T) {
	tr, _ := newReserveTrial(spentResults{total: 0.0003, arm: 0.0003})
	cfg := jevtrialsettings.Settings{CapUSD: 1}
	arm := jevtrialsettings.Arm{ID: "a1", DailyCostCapUSD: 0.0015}
	now := time.Now()
	first, _, err := tr.mayCall(context.Background(), cfg, arm, now, 0.001)
	if err != nil || first == nil {
		t.Fatalf("first = %v %v: 0.0003 stored + 0.001 reserved fits under 0.0015", first, err)
	}
	if second, skip, err := tr.mayCall(context.Background(), cfg, arm, now, 0.001); err != nil || second != nil || skip != jevtrial.SkipDailyCapArm {
		t.Fatalf("second = %v %v %q: 0.0003 + 2 × 0.001 exceeds the arm's cap (skip reason daily_cap_arm)", second, err, skip)
	}
	other, _, err := tr.mayCall(context.Background(), cfg, jevtrialsettings.Arm{ID: "a2", DailyCostCapUSD: 0.0015}, now, 0.001)
	if err != nil || other == nil {
		t.Fatalf("another arm = %v %v: only its own reservations count against its cap, 0.0003 + 0.001 fits", other, err)
	}
}
