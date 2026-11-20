package jevreplay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// scope says what a block holds back.
type scope int

const (
	// scopeArm: this arm gets no call now; the others may go on.
	scopeArm scope = iota + 1
	// scopeOverall: no arm gets a call now (the overall cap).
	scopeOverall
	// scopeRun: the run itself ends (its own cost cap).
	scopeRun
)

// block is why a call is not made now.
type block struct {
	reason string
	scope  scope
}

// reservation is the worst-case cost of a call that is about to be made or is in
// flight, held against the caps from the moment the call is allowed until its
// row is stored (the trial's mechanism, reserve.go there): the daily caps are
// judged on stored rows, which do not include a call that has not finished, so
// without it up to maxInFlight calls could all pass the same check. It is held
// in memory only: after a restart no call is in flight.
type reservation struct {
	r   *Runner
	arm string
	usd float64
}

func (r *Runner) reserve(arm string, usd float64) *reservation {
	r.resMu.Lock()
	defer r.resMu.Unlock()
	r.reservedTotal += usd
	r.reservedArm[arm] += usd
	return &reservation{r: r, arm: arm, usd: usd}
}

// reserved returns the amount reserved over all arms and for arm (this call's
// own reservation included). It is read BEFORE the stored cost: a call that
// stores its row and releases in between is then counted twice (the safe side),
// never zero times.
func (r *Runner) reserved(arm string) (total, byArm float64) {
	r.resMu.Lock()
	defer r.resMu.Unlock()
	return r.reservedTotal, r.reservedArm[arm]
}

// release gives the reservation back; call it after the call's row is stored (or
// the call is abandoned). A nil reservation and a second call are no-ops.
func (res *reservation) release() {
	if res == nil || res.r == nil {
		return
	}
	r := res.r
	res.r = nil
	r.resMu.Lock()
	defer r.resMu.Unlock()
	r.reservedTotal -= res.usd
	r.reservedArm[res.arm] -= res.usd
	if r.reservedArm[res.arm] <= 0 {
		delete(r.reservedArm, res.arm)
	}
	if len(r.reservedArm) == 0 {
		r.reservedTotal = 0 // no float drift once nothing is in flight
	}
}

// gate runs the checks that cost no request for one call of arm, then reserves
// reserveUSD for it. A nil reservation with a nil block never happens: either the
// call is allowed (a reservation) or it is blocked (b != nil), or the costs could
// not be read (err; then the arm is not called).
//
// The checks are the trial's (trial.mayCall) and read the trial's stop records
// (never write them): a stop of the whole trial or of the arm for today, the
// arm's cooldown after a 429/529, the arm's failed calls of the day, then the
// overall and the arm's daily cost cap against the counted cost of the TRIAL's
// rows plus the replay's rows plus the reservations of the calls in flight and of
// this call, and last the run's own cost cap (its estimate's worst case).
func (e *execution) gate(ctx context.Context, arm jevtrialsettings.Arm, reserveUSD float64) (*reservation, *block, error) {
	r := e.r
	now := r.now()
	day := jevshadowsettings.Day(now)
	from, to := jevshadowsettings.DayBounds(now)
	sctx, cancel := context.WithTimeout(ctx, settingsTimeout)
	st := jevtrialsettings.LoadArm(sctx, r.settings, arm.ID)
	cancel()
	if st.ReadErr != nil {
		// "No record" is not the safe side when the read failed: a stopped arm would be called.
		return nil, nil, fmt.Errorf("jev replay: read the stop records of arm %q (not calling it): %w", arm.ID, st.ReadErr)
	}
	switch {
	case e.cfg.Halt.HaltOn(day):
		return nil, &block{haltSkip(e.cfg.Halt.Reason), scopeOverall}, nil
	case st.Halt.HaltOn(day), r.haltHeld(arm.ID, day):
		return nil, &block{haltSkipArm(st.Halt), scopeArm}, nil
	case st.Throttle.ActiveAt(now), r.throttleHeld(arm.ID, now):
		return nil, &block{jevtrial.SkipThrottled, scopeArm}, nil
	}
	// Reserve before reading the stored cost (see reserved), and give it back
	// unless the call is allowed.
	held := r.reserve(arm.ID, reserveUSD)
	allowed := false
	defer func() {
		if !allowed {
			held.release()
		}
	}()
	reservedTotal, reservedArm := r.reserved(arm.ID)
	total, armSpent, err := r.spent(ctx, arm.ID, from, to)
	if err != nil {
		return nil, nil, err
	}
	capUSD, armCap := e.cfg.CapUSD, e.armCap(arm)
	failed, err := r.store.FailedByArmBetween(ctx, arm.ID, from, to)
	if err != nil {
		return nil, nil, fmt.Errorf("jev replay: count today's failed calls of arm %q (not calling it): %w", arm.ID, err)
	}
	switch {
	case total >= capUSD:
		return nil, &block{jevtrial.SkipDailyCapOverall, scopeOverall}, nil
	case armSpent >= armCap:
		return nil, &block{jevtrial.SkipDailyCapArm, scopeArm}, nil
	case failed >= jevtrialsettings.DailyFailureCap:
		return nil, &block{jevtrial.SkipStoppedFailureCap, scopeArm}, nil
	case total+reservedTotal > capUSD:
		return nil, &block{jevtrial.SkipDailyCapOverall, scopeOverall}, nil
	case armSpent+reservedArm > armCap:
		return nil, &block{jevtrial.SkipDailyCapArm, scopeArm}, nil
	}
	// The run never counts more than its estimate's worst case (a small slack for float sums).
	if e.runSpent()+reservedTotal > e.run.Estimate.MaxCostUSD+1e-9 {
		return nil, &block{jevreplay.StopRunCostCap, scopeRun}, nil
	}
	allowed = true
	return held, nil, nil
}

// spent is the cost counted today over every arm and for arm: the trial's rows
// plus the replay's rows (the caps are shared).
func (r *Runner) spent(ctx context.Context, arm string, from, to time.Time) (total, byArm float64, err error) {
	trialAll, err := r.trialSpend.SpentBetween(ctx, from, to)
	if err != nil {
		return 0, 0, fmt.Errorf("jev replay: read today's trial cost (not calling arm %q): %w", arm, err)
	}
	replayAll, err := r.store.SpentBetween(ctx, from, to)
	if err != nil {
		return 0, 0, fmt.Errorf("jev replay: read today's replay cost (not calling arm %q): %w", arm, err)
	}
	trialArm, err := r.trialSpend.SpentByArmBetween(ctx, arm, from, to)
	if err != nil {
		return 0, 0, fmt.Errorf("jev replay: read today's trial cost of arm %q (not calling it): %w", arm, err)
	}
	replayArm, err := r.store.SpentByArmBetween(ctx, arm, from, to)
	if err != nil {
		return 0, 0, fmt.Errorf("jev replay: read today's replay cost of arm %q (not calling it): %w", arm, err)
	}
	return trialAll.CountedCostUSD + replayAll.CountedCostUSD, trialArm.CountedCostUSD + replayArm.CountedCostUSD, nil
}

// armCap is the arm's daily cap: the currently configured arm's, else the run's
// snapshot (the arm was removed from the settings since the run started).
func (e *execution) armCap(arm jevtrialsettings.Arm) float64 {
	for _, cur := range e.cfg.Arms {
		if cur.ID == arm.ID {
			return cur.CapUSD()
		}
	}
	return arm.CapUSD()
}

// haltSkip maps a trial stop record's reason (jevtrialsettings.HaltReason*) to a
// blocked reason, as the trial does.
func haltSkip(reason string) string {
	switch reason {
	case jevtrialsettings.HaltReasonDailyCostCap:
		return jevtrial.SkipDailyCapOverall
	case jevtrialsettings.HaltReasonPaymentRequired:
		return jevtrial.SkipStoppedPaymentRequired
	}
	return jevtrial.SkipStoppedOther
}

// haltSkipArm is the blocked reason of an arm's stop record (nil: the arm is
// held in memory by a 403 of this process).
func haltSkipArm(h *jevtrialsettings.Halt) string {
	if h == nil {
		return jevtrial.SkipStoppedForbidden
	}
	switch h.Reason {
	case jevtrialsettings.HaltReasonArmCostCap:
		return jevtrial.SkipDailyCapArm
	case jevtrialsettings.HaltReasonDailyFailureCap:
		return jevtrial.SkipStoppedFailureCap
	case jevtrialsettings.HaltReasonForbidden:
		return jevtrial.SkipStoppedForbidden
	case jevtrialsettings.HaltReasonPaymentRequired:
		return jevtrial.SkipStoppedPaymentRequired
	case jevtrialsettings.HaltReasonDailyCostCap:
		return jevtrial.SkipDailyCapOverall
	}
	return jevtrial.SkipStoppedOther
}

// book reserves arm's next call slot and returns how long to wait for it: at
// least minInterval after the arm's previous call. Pacing is per arm (arms have
// their own limits); a replay waits instead of skipping.
func (r *Runner) book(arm string) time.Duration {
	r.paceMu.Lock()
	defer r.paceMu.Unlock()
	now := r.pace()
	at := now
	if last, ok := r.lastCall[arm]; ok && r.minInterval > 0 {
		if next := last.Add(r.minInterval); next.After(now) {
			at = next
		}
	}
	r.lastCall[arm] = at
	return at.Sub(now)
}

// haltHeld reports whether arm was stopped by a 403 on the JST day day, in
// memory (a replay never writes the trial's stop records).
func (r *Runner) haltHeld(arm, day string) bool {
	r.paceMu.Lock()
	defer r.paceMu.Unlock()
	return r.halted[arm] == day
}

func (r *Runner) holdHalt(arm, day string) {
	r.paceMu.Lock()
	defer r.paceMu.Unlock()
	r.halted[arm] = day
	slog.Warn("jev replay: the arm is stopped for the day (HTTP 403); the live decisions continue", "arm", arm, "date", day)
}

// throttleHeld reports whether arm's cooldown after a 429/529 is still running.
func (r *Runner) throttleHeld(arm string, now time.Time) bool {
	r.paceMu.Lock()
	defer r.paceMu.Unlock()
	return now.Before(r.throttledUntil[arm])
}

func (r *Runner) holdThrottle(arm string, until time.Time) {
	r.paceMu.Lock()
	defer r.paceMu.Unlock()
	if until.After(r.throttledUntil[arm]) {
		r.throttledUntil[arm] = until
	}
	slog.Warn("jev replay: the arm's API is throttling; the arm waits and is never retried at once", "arm", arm, "until", until.UTC().Format(time.RFC3339))
}
