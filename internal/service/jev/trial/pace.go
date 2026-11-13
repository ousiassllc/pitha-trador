package trial

import (
	"time"
)

// Option adjusts a Trial.
type Option func(*Trial)

// WithMinInterval replaces the least time between two calls of one arm
// (jevtrialsettings.MinCallInterval); 0 turns pacing off. Tests use it.
func WithMinInterval(d time.Duration) Option { return func(t *Trial) { t.minInterval = d } }

// WithPaceClock replaces the clock that paces the calls (time.Now). Tests use it.
func WithPaceClock(now func() time.Time) Option { return func(t *Trial) { t.pace = now } }

// allowCall reports whether arm may call its API now: at least minInterval
// after the arm's previous call. It books the call, so concurrent comparisons
// of one arm cannot both pass. Pacing is per arm: arms have their own keys.
func (t *Trial) allowCall(arm string) bool {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	now := t.pace()
	if last, ok := t.lastCall[arm]; ok && t.minInterval > 0 && now.Sub(last) < t.minInterval {
		return false
	}
	t.lastCall[arm] = now
	return true
}

// throttleHeld reports whether arm's in-memory cooldown set by recordThrottle is
// still running at now. It holds even when the throttle record could not be
// written, so a failing runtime_settings write cannot let the arm send again.
func (t *Trial) throttleHeld(arm string, now time.Time) bool {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	return now.Before(t.throttledUntil[arm])
}

// holdThrottle extends arm's in-memory cooldown to until (never shortens it).
func (t *Trial) holdThrottle(arm string, until time.Time) {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	if until.After(t.throttledUntil[arm]) {
		t.throttledUntil[arm] = until
	}
}

// haltHeld reports whether key (KeyHalt or an arm's HaltKey) was stopped by a
// 402/403 on the JST day day, in memory.
func (t *Trial) haltHeld(key, day string) bool {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	return t.halted[key] == day
}

// holdHalt remembers a 402/403 stop of key for the JST day day.
func (t *Trial) holdHalt(key, day string) {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	t.halted[key] = day
}
