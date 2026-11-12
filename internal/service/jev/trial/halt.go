package trial

import (
	"context"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// recordCtx is a short context for a stop record or counter write. It is not
// the call's: that may be the one that timed out or was cancelled.
func (t *Trial) recordCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(t.base), settingsTimeout)
}

// stopAfterFailure applies the stop a failed row calls for:
//   - HTTP 402: the account is out of credit; the key is shared, so the whole
//     trial stops for the day (KeyHalt).
//   - HTTP 403: that arm stops for the day (the key, the model or the provider
//     preference is not allowed for it).
//   - HTTP 429/529: that arm stops for jevtrialsettings.ThrottleCooldown and is
//     never retried. Nothing was answered, so it is not a failed call against
//     DailyFailureCap.
//   - anything else: one failed call is added to the arm's day counter.
func (t *Trial) stopAfterFailure(arm jevtrialsettings.Arm, d jevtrial.Decision, now time.Time) {
	status := 0
	if d.HTTPStatus != nil {
		status = *d.HTTPStatus
	}
	switch {
	case status == 402:
		t.recordHalt(jevtrialsettings.KeyHalt, jevtrialsettings.Halt{Reason: jevtrialsettings.HaltReasonPaymentRequired, Status: status}, now)
	case status == 403:
		t.recordHalt(jevtrialsettings.HaltKey(arm.ID), jevtrialsettings.Halt{Reason: jevtrialsettings.HaltReasonForbidden, Status: status}, now)
	case jevtrialsettings.IsThrottleStatus(status):
		t.recordThrottle(arm.ID, status, now)
	default:
		t.countFailure(arm.ID, jevshadowsettings.Day(now))
	}
}

// recordHalt leaves why the trial (key = KeyHalt) or an arm (key = HaltKey) is
// stopped for the JST day of now (h: the reason with its numbers), once per day:
// the first reason of a day stays. A failed write is logged; the stop still
// holds because the caps are checked against stored state before every call.
func (t *Trial) recordHalt(key string, h jevtrialsettings.Halt, now time.Time) {
	ctx, cancel := t.recordCtx()
	defer cancel()
	h.Date, h.At = jevshadowsettings.Day(now), now.UTC().Format(time.RFC3339)
	t.recMu.Lock()
	defer t.recMu.Unlock()
	if raw, ok, err := t.settings.Get(ctx, key); err == nil && ok {
		if cur, perr := jevtrialsettings.ParseHalt(key, raw); perr == nil && cur.HaltOn(h.Date) {
			return
		}
	}
	value, err := jevtrialsettings.EncodeHalt(key, h)
	if err == nil {
		err = t.settings.Set(ctx, key, value, now)
	}
	if err != nil {
		slog.Error("jev trial: record why the trial stopped for the day", "key", key, "reason", h.Reason, "error", err)
		return
	}
	slog.Warn("jev trial: stopped for the day; the live decisions continue",
		"key", key, "date", h.Date, "reason", h.Reason, "status", h.Status, "spent_usd", h.SpentUSD, "cap_usd", h.CapUSD, "failures", h.Failures)
}

// countFailure adds one failed call to arm's stored counter of the JST day day.
// A failed write is logged; the next failure counts on.
func (t *Trial) countFailure(arm, day string) {
	ctx, cancel := t.recordCtx()
	defer cancel()
	t.recMu.Lock()
	defer t.recMu.Unlock()
	f := jevtrialsettings.Failures{Date: day}
	if stored := jevtrialsettings.LoadArm(ctx, t.settings, arm).Failures; stored != nil && stored.Date == day {
		f.Count = stored.Count
	}
	f.Count++
	key := jevtrialsettings.FailuresKey(arm)
	value, err := jevtrialsettings.EncodeFailures(key, f)
	if err == nil {
		err = t.settings.Set(ctx, key, value, t.now())
	}
	if err != nil {
		slog.Error("jev trial: count the arm's failed call", "arm", arm, "error", err)
	}
}

// recordThrottle leaves why arm stops for jevtrialsettings.ThrottleCooldown from
// at (status: its call's 429/529), and counts the day's events. The stop also
// holds in memory (throttleHeld). A failed write is logged; the in-memory stop
// still holds, and the next 429/529 records again.
func (t *Trial) recordThrottle(arm string, status int, at time.Time) {
	ctx, cancel := t.recordCtx()
	defer cancel()
	t.holdThrottle(arm, at.Add(jevtrialsettings.ThrottleCooldown))
	t.recMu.Lock()
	defer t.recMu.Unlock()
	cur := jevtrialsettings.LoadArm(ctx, t.settings, arm).Throttle
	if cur.ActiveAt(at) {
		return // another comparison of this arm already recorded this stop
	}
	day := jevshadowsettings.Day(at)
	rec := jevtrialsettings.Throttle{
		Date: day, Count: cur.CountOn(day) + 1, Status: status, Reason: jevtrialsettings.ThrottleReason(status),
		At: at.UTC().Format(time.RFC3339), Until: at.Add(jevtrialsettings.ThrottleCooldown).UTC().Format(time.RFC3339),
	}
	key := jevtrialsettings.ThrottleKey(arm)
	value, err := jevtrialsettings.EncodeThrottle(key, rec)
	if err == nil {
		err = t.settings.Set(ctx, key, value, t.now())
	}
	if err != nil {
		slog.Error("jev trial: record why the arm stopped on a 429/529", "arm", arm, "status", status, "error", err)
		return
	}
	slog.Warn("jev trial: the arm's API is throttling; the arm stops for a while and the live decisions continue",
		"arm", arm, "status", status, "until", rec.Until, "events_today", rec.Count)
}
