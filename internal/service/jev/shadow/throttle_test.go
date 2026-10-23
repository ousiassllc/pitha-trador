package shadow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// memSettings is an in-memory Settings; failSet makes every Set fail.
type memSettings struct {
	mu      sync.Mutex
	values  map[string]string
	failSet bool
}

func (m *memSettings) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memSettings) Set(_ context.Context, key, value string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSet {
		return context.DeadlineExceeded
	}
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[key] = value
	return nil
}

func (m *memSettings) throttle(t *testing.T) *jevshadowsettings.Throttle {
	t.Helper()
	return jevshadowsettings.Load(context.Background(), m).Throttle
}

// recordConcurrently runs n recordThrottle calls at once, each starting from the
// same stale settings (no throttle), as n comparisons that read them before any
// of them recorded.
func recordConcurrently(s *Shadow, n int, source string, at func(i int) time.Time) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.recordThrottle(context.Background(), jevshadowsettings.Settings{}, 429, source, at(i))
		}()
	}
	close(start)
	wg.Wait()
}

// Several comparisons that saw the same 429 (a live one: the same
// Client.LastThrottle event) count one event, not one each.
func TestRecordThrottle_ConcurrentComparisonsCountOneLiveEvent(t *testing.T) {
	store := &memSettings{}
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	s := New(nil, nil, store, metalabelsettings.ModePaper, func() time.Time { return now })
	event := now.Add(-3*time.Second + 123*time.Millisecond)

	recordConcurrently(s, 8, jevshadowsettings.ThrottleSourceLive, func(int) time.Time { return event })

	th := store.throttle(t)
	if th == nil || th.Count != 1 || th.Source != jevshadowsettings.ThrottleSourceLive {
		t.Fatalf("throttle record = %+v, want one live event", th)
	}
	// Seeing that event again later does not count it again either.
	s.recordThrottle(context.Background(), jevshadowsettings.Settings{}, 429, jevshadowsettings.ThrottleSourceLive, event)
	if th := store.throttle(t); th.Count != 1 {
		t.Errorf("count after seeing the same event again = %d, want 1", th.Count)
	}
}

// Calls of the shadow that were in flight together and all got 429 are one stop.
func TestRecordThrottle_ConcurrentOwn429sCountOneEvent(t *testing.T) {
	store := &memSettings{}
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	s := New(nil, nil, store, metalabelsettings.ModePaper, func() time.Time { return now })

	recordConcurrently(s, 8, jevshadowsettings.ThrottleSourceShadow, func(i int) time.Time {
		return now.Add(time.Duration(i) * 50 * time.Millisecond)
	})

	if th := store.throttle(t); th == nil || th.Count != 1 {
		t.Fatalf("throttle record = %+v, want one event", th)
	}
}

// A later, separate event (after the cooldown) is counted again.
func TestRecordThrottle_CountsALaterEvent(t *testing.T) {
	store := &memSettings{}
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	s := New(nil, nil, store, metalabelsettings.ModePaper, func() time.Time { return now })

	s.recordThrottle(context.Background(), jevshadowsettings.Settings{}, 429, jevshadowsettings.ThrottleSourceShadow, now)
	later := now.Add(jevshadowsettings.ThrottleCooldown)
	s.recordThrottle(context.Background(), jevshadowsettings.Settings{}, 429, jevshadowsettings.ThrottleSourceShadow, later)

	if th := store.throttle(t); th == nil || th.Count != 2 {
		t.Fatalf("throttle record = %+v, want two events", th)
	}
}

// The stop holds in memory when the record cannot be written.
func TestRecordThrottle_HoldsInMemoryWhenTheWriteFails(t *testing.T) {
	store := &memSettings{failSet: true}
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	s := New(nil, nil, store, metalabelsettings.ModePaper, func() time.Time { return now })

	if s.throttleHeld(now) {
		t.Fatal("held before any 429")
	}
	s.recordThrottle(context.Background(), jevshadowsettings.Settings{}, 429, jevshadowsettings.ThrottleSourceShadow, now)

	if th := store.throttle(t); th != nil {
		t.Fatalf("a record was stored although the write fails: %+v", th)
	}
	if !s.throttleHeld(now.Add(jevshadowsettings.ThrottleCooldown - time.Second)) {
		t.Error("not held inside the cooldown")
	}
	if s.throttleHeld(now.Add(jevshadowsettings.ThrottleCooldown)) {
		t.Error("still held after the cooldown")
	}
}
