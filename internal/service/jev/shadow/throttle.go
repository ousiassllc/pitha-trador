package shadow

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
)

// Option adjusts a Shadow.
type Option func(*Shadow)

// WithMinInterval replaces the least time between two shadow calls
// (jevshadowsettings.MinCallInterval); 0 turns pacing off. Tests use it.
func WithMinInterval(d time.Duration) Option { return func(s *Shadow) { s.minInterval = d } }

// WithPaceClock replaces the clock that paces the calls (time.Now). Tests use it.
func WithPaceClock(now func() time.Time) Option { return func(s *Shadow) { s.pace = now } }

// allowCall reports whether the shadow may call the API now: at least minInterval
// after the previous call. It books the call, so concurrent comparisons cannot
// both pass.
func (s *Shadow) allowCall() bool {
	s.paceMu.Lock()
	defer s.paceMu.Unlock()
	t := s.pace()
	if s.minInterval > 0 && !s.lastCall.IsZero() && t.Sub(s.lastCall) < s.minInterval {
		return false
	}
	s.lastCall = t
	return true
}

// recordThrottle leaves why the shadow stops for jevshadowsettings.ThrottleCooldown
// from at (the 429/529 status of source's call), so the GUI and the MCP can show
// it, and counts the day's events. It writes with its own short context: the
// call's may be the one that was cancelled. A failed write is logged; the
// shadow is not stopped by it (the next 429/529 stops it again).
func (s *Shadow) recordThrottle(ctx context.Context, cfg jevshadowsettings.Settings, status int, source string, at time.Time) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.throttleMu.Lock()
	defer s.throttleMu.Unlock()
	// Another comparison may have recorded since cfg was read.
	if cur := jevshadowsettings.Load(ctx, s.settings).Throttle; cur != nil {
		cfg.Throttle = cur
	}
	day := jevshadowsettings.Day(at)
	t := jevshadowsettings.Throttle{
		Date: day, Count: cfg.Throttle.CountOn(day) + 1, Status: status, Reason: jevshadowsettings.ThrottleReason(status), Source: source,
		At: at.UTC().Format(time.RFC3339), Until: at.Add(jevshadowsettings.ThrottleCooldown).UTC().Format(time.RFC3339),
	}
	value, err := jevshadowsettings.EncodeThrottle(t)
	if err == nil {
		err = s.settings.Set(ctx, jevshadowsettings.KeyThrottle, value, s.now())
	}
	if err != nil {
		slog.Error("jev shadow: record why the shadow stopped on a 429/529", "status", status, "error", err)
		return
	}
	slog.Warn("jev shadow: the Jev API is throttling; the shadow stops for a while and the live decisions continue",
		"status", status, "source", source, "until", t.Until, "events_today", t.Count)
}
