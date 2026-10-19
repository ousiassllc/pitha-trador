package healthalert_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/healthalert"
)

// memStore keeps what the Detector synced, as the real store would.
type memStore struct {
	alerts    []domain.HealthAlert
	checkedAt time.Time
	syncs     int
}

func (s *memStore) Sync(_ context.Context, now time.Time, alerts []domain.HealthAlert) error {
	s.alerts, s.checkedAt = alerts, now
	s.syncs++
	return nil
}

func (s *memStore) Active(context.Context) ([]domain.HealthAlert, error) { return s.alerts, nil }

type fakeJobs struct {
	last time.Time
	err  error
}

func (j *fakeJobs) LastFinishedAt(context.Context, string) (time.Time, bool, error) {
	return j.last, !j.last.IsZero(), j.err
}

// rig is a Detector over a fake clock, store and jobs.
type rig struct {
	now   time.Time
	store *memStore
	jobs  *fakeJobs
	token healthalert.TokenState
	d     *healthalert.Detector
}

func newRig(start time.Time) *rig {
	r := &rig{now: start, store: &memStore{}, jobs: &fakeJobs{}}
	r.d = healthalert.New(healthalert.Config{
		Store: r.store, Jobs: r.jobs,
		Token: func() healthalert.TokenState { return r.token },
		Now:   func() time.Time { return r.now },
	})
	return r
}

func (r *rig) check(t *testing.T, at time.Time) []domain.HealthAlert {
	t.Helper()
	r.now = at
	if err := r.d.Check(context.Background()); err != nil {
		t.Fatalf("Check at %v: %v", at, err)
	}
	return r.store.alerts
}

func codesOf(alerts []domain.HealthAlert) []string {
	out := []string{}
	for _, a := range alerts {
		out = append(out, a.Code)
	}
	return out
}

func TestDetector_KabuNotLoggedInAt0845RaisesThenClearsOnLogin(t *testing.T) {
	r := newRig(at(20, 8, 0))
	r.token = healthalert.TokenState{NotLoggedInSince: at(19, 20, 0)}
	r.jobs.last = at(20, 8, 40) // the market-data pipeline is alive

	if got := r.check(t, at(20, 8, 44)); len(got) != 0 {
		t.Fatalf("08:44 alerts = %v, want none before the 08:45 check", codesOf(got))
	}
	got := r.check(t, at(20, 8, 45))
	if len(got) != 1 || got[0].Code != domain.HealthAlertKabuNotLoggedIn || !got[0].Since.Equal(at(20, 8, 45)) {
		t.Fatalf("08:45 alerts = %+v, want kabu_not_logged_in since 08:45", got)
	}
	r.jobs.last = at(20, 9, 29)
	if got = r.check(t, at(20, 9, 30)); len(got) != 1 || !got[0].Since.Equal(at(20, 8, 45)) {
		t.Fatalf("09:30 alerts = %+v, want the same Since while the condition lasts", got)
	}

	r.token = healthalert.TokenState{} // the operator logged in
	r.jobs.last = at(20, 9, 30)
	if got = r.check(t, at(20, 9, 31)); len(got) != 0 {
		t.Fatalf("after login alerts = %v, want none (cleared automatically)", codesOf(got))
	}
	if !r.store.checkedAt.Equal(at(20, 9, 31)) {
		t.Errorf("heartbeat = %v, want the last check time", r.store.checkedAt)
	}
}

func TestDetector_MidSessionLogoutNeedsFiveMinutes(t *testing.T) {
	r := newRig(at(20, 9, 0))
	r.jobs.last = at(20, 10, 0)
	r.token = healthalert.TokenState{NotLoggedInSince: at(20, 10, 0)}
	if got := r.check(t, at(20, 10, 4)); len(got) != 0 {
		t.Fatalf("after 4 minutes alerts = %v, want none", codesOf(got))
	}
	r.jobs.last = at(20, 10, 4)
	got := r.check(t, at(20, 10, 5))
	if len(got) != 1 || got[0].Code != domain.HealthAlertKabuNotLoggedIn || !got[0].Since.Equal(at(20, 10, 0)) {
		t.Fatalf("after 5 minutes alerts = %+v, want kabu_not_logged_in since 10:00", got)
	}
}

func TestDetector_MarketDataIdleRaisesAfterFiveQuietMinutesAndClearsOnActivity(t *testing.T) {
	r := newRig(at(20, 9, 0))
	r.jobs.last = at(20, 10, 0)
	if got := r.check(t, at(20, 10, 4)); len(got) != 0 {
		t.Fatalf("10:04 alerts = %v, want none", codesOf(got))
	}
	got := r.check(t, at(20, 10, 5))
	if len(got) != 1 || got[0].Code != domain.HealthAlertMarketDataIdle || !got[0].Since.Equal(at(20, 10, 0)) {
		t.Fatalf("10:05 alerts = %+v, want market_data_idle since 10:00", got)
	}
	r.jobs.last = at(20, 10, 6) // a job finished, success or failure
	if got = r.check(t, at(20, 10, 7)); len(got) != 0 {
		t.Fatalf("after a job finished alerts = %v, want none", codesOf(got))
	}
}

func TestDetector_AFreshStartGetsTheFullIdleGraceInsideASession(t *testing.T) {
	// The app starts at 10:00 with the last job from the morning: the five
	// minutes count from the start, not from a job that finished before it.
	r := newRig(at(20, 10, 0))
	r.jobs.last = at(20, 9, 10)
	if got := r.check(t, at(20, 10, 4)); len(got) != 0 {
		t.Fatalf("4 minutes after start alerts = %v, want none", codesOf(got))
	}
	if got := r.check(t, at(20, 10, 5)); len(got) != 1 || !got[0].Since.Equal(at(20, 10, 0)) {
		t.Fatalf("5 minutes after start alerts = %+v, want market_data_idle since the start", got)
	}
}

func TestDetector_NoAlertsOnNonTradingDaysOrOutsideTheHours(t *testing.T) {
	r := newRig(at(24, 8, 0))
	r.token = healthalert.TokenState{NotLoggedInSince: at(23, 20, 0)}
	for _, now := range []time.Time{at(24, 8, 45), at(24, 10, 0), at(24, 13, 0), at(12, 10, 0)} {
		if got := r.check(t, now); len(got) != 0 {
			t.Errorf("non-trading day %v alerts = %v, want none", now, codesOf(got))
		}
	}
	for _, now := range []time.Time{at(20, 3, 0), at(20, 8, 44), at(20, 15, 30), at(20, 22, 0)} {
		if got := r.check(t, now); len(got) != 0 {
			t.Errorf("trading day outside the hours %v alerts = %v, want none", now, codesOf(got))
		}
	}
}

// The lunch break: kabu keeps alerting, market data is not expected.
func TestDetector_LunchBreakKeepsKabuButNotIdle(t *testing.T) {
	r := newRig(at(20, 8, 0))
	r.token = healthalert.TokenState{NotLoggedInSince: at(20, 8, 0)}
	r.jobs.last = at(20, 11, 0)
	got := r.check(t, at(20, 12, 0))
	if len(got) != 1 || got[0].Code != domain.HealthAlertKabuNotLoggedIn {
		t.Errorf("lunch break alerts = %v, want only kabu_not_logged_in", codesOf(got))
	}
}

func TestDetector_ActiveHidesStoredAlertsOutsideTheirWindow(t *testing.T) {
	r := newRig(at(20, 9, 0))
	r.store.alerts = []domain.HealthAlert{
		{Code: domain.HealthAlertKabuNotLoggedIn, Since: at(20, 8, 45)},
		{Code: domain.HealthAlertMarketDataIdle, Since: at(20, 9, 30)},
	}
	for now, want := range map[time.Time]int{at(20, 10, 0): 2, at(20, 12, 0): 1, at(20, 15, 30): 0, at(24, 10, 0): 0} {
		r.now = now
		got, err := r.d.Active(context.Background())
		if err != nil || len(got) != want {
			t.Errorf("Active at %v = %v, %v; want %d alerts", now, codesOf(got), err, want)
		}
	}
}

func TestDetector_CheckReportsAJobReadFailureAndStoresNothing(t *testing.T) {
	r := newRig(at(20, 10, 0))
	r.jobs.err = errors.New("db down")
	r.now = at(20, 10, 10)
	if err := r.d.Check(context.Background()); err == nil {
		t.Fatal("Check succeeded despite a job read failure")
	}
	if r.store.syncs != 0 {
		t.Errorf("store synced %d times after a read failure, want 0 (a stale heartbeat must not look healthy)", r.store.syncs)
	}
}
