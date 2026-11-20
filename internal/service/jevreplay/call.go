package jevreplay

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trial"
)

// execution is one pass over one run: the calls still to make, the in-flight
// ones and what they learned (a held arm, a run-ending status).
type execution struct {
	r       *Runner
	run     jevreplay.Run
	cfg     jevtrialsettings.Settings
	plan    Plan
	callers map[string]Caller
	wg      sync.WaitGroup

	mu       sync.Mutex
	spent    float64           // the run's counted cost: stored rows, plus calls whose row could not be stored
	armBlock map[string]string // arm id -> why it gets no call in this pass
	end      *ending           // set when the run itself must end
	abort    bool              // a result could not be stored: no further call
	inflight int
}

// ending is a run-ending status and its reason.
type ending struct{ status, reason string }

func (e *execution) runSpent() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spent
}

func (e *execution) inFlight() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inflight
}

func (e *execution) over() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.end != nil || e.abort
}

func (e *execution) setEnd(status, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.end == nil {
		e.end = &ending{status, reason}
	}
}

func (e *execution) blockArm(arm, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.armBlock[arm] = reason
}

func (e *execution) blockedArm(arm string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.armBlock[arm]
}

// capBlock reports a block that in-flight reservations can cause: it is judged
// again once the calls in flight have stored their rows.
func (b *block) capBlock() bool {
	return b.reason == jevtrial.SkipDailyCapOverall || b.reason == jevtrial.SkipDailyCapArm || b.reason == jevreplay.StopRunCostCap
}

// dispatch makes the calls of work in order, each after its checks (stop flag,
// pacing, gate), with at most maxInFlight at once, and then records how the pass
// ended: the run completed, stopped, or is held back (blocked_reason) until a
// later pass. Once a cap is reached no new call is issued.
func (e *execution) dispatch(ctx context.Context, work []Call) error {
	r := e.r
	var (
		overall string
		gateErr error
		started bool
	)
loop:
	for _, c := range work {
		if ctx.Err() != nil || e.over() {
			break
		}
		arm := e.plan.Arms[c.Arm].Arm
		if e.blockedArm(arm.ID) != "" {
			continue
		}
		stopped, err := r.store.StopRequested(ctx, e.run.ID)
		if err != nil {
			gateErr = err
			break
		}
		if stopped {
			e.setEnd(jevreplay.StatusStopped, jevreplay.StopRequested)
			break
		}
		if d := r.book(arm.ID); d > 0 {
			if err := r.sleep(ctx, d); err != nil {
				break
			}
		}
		// Read before the gate: a call that finishes while the gate runs may still have been
		// counted both as stored and as reserved, and must not hide that from the re-judging.
		held := e.inFlight() > 0
		res, b, err := e.gate(ctx, arm, c.ReservedUSD)
		if b != nil && b.capBlock() && (held || e.inFlight() > 0) {
			// The calls in flight are held at their worst case: judge again on the stored rows alone.
			e.wg.Wait()
			res, b, err = e.gate(ctx, arm, c.ReservedUSD)
		}
		switch {
		case err != nil:
			gateErr = err
			break loop
		case b == nil:
		case b.scope == scopeArm:
			e.blockArm(arm.ID, b.reason)
			continue loop
		case b.scope == scopeOverall:
			overall = b.reason
			break loop
		default:
			e.setEnd(jevreplay.StatusStopped, b.reason)
			break loop
		}
		select {
		case r.slots <- struct{}{}:
		case <-ctx.Done():
			res.release()
			break loop
		}
		if !started {
			started = true
			e.clearBlocked(ctx)
		}
		e.launch(ctx, arm, e.plan.Decisions[c.Decision], res)
	}
	e.wg.Wait()
	return e.conclude(ctx, overall, gateErr)
}

// conclude records how a pass ended. A pass cut off by ctx (a shutdown) records
// nothing: the run stays as it is and continues, without repeating a recorded
// call, at the next start.
func (e *execution) conclude(ctx context.Context, overall string, gateErr error) error {
	r := e.r
	if ctx.Err() != nil {
		return nil
	}
	e.mu.Lock()
	end, abort := e.end, e.abort
	var armReason string
	if len(e.armBlock) > 0 {
		arms := make([]string, 0, len(e.armBlock))
		for a := range e.armBlock {
			arms = append(arms, a)
		}
		slices.Sort(arms)
		armReason = e.armBlock[arms[0]]
	}
	e.mu.Unlock()
	switch {
	case end != nil:
		return r.finish(ctx, e.run.ID, end.status, end.reason)
	case abort:
		return errors.New("jev replay: a result could not be stored; the run continues at the next pass")
	case gateErr != nil:
		e.setBlocked(ctx, jevtrial.SkipCheckFailed)
		return gateErr
	case overall != "":
		e.setBlocked(ctx, overall)
		return nil
	case armReason != "":
		e.setBlocked(ctx, armReason)
		return nil
	}
	return r.finish(ctx, e.run.ID, jevreplay.StatusCompleted, "")
}

func (e *execution) setBlocked(ctx context.Context, reason string) {
	rctx, cancel := e.r.recordCtx()
	defer cancel()
	if err := e.r.store.SetBlocked(rctx, e.run.ID, reason, e.r.now()); err != nil {
		slog.Warn("jev replay: record why the run waits", "run_id", e.run.ID, "reason", reason, "error", err)
	}
}

// clearBlocked records that the run makes calls again.
func (e *execution) clearBlocked(ctx context.Context) {
	if e.run.BlockedReason != "" {
		e.setBlocked(ctx, "")
	}
}

// launch makes one call in its own goroutine: panics are recovered (the pair is
// asked again at the next pass), the slot and the reservation are given back and
// a failure is a log line.
func (e *execution) launch(ctx context.Context, arm jevtrialsettings.Arm, d repo.SavedDecision, res *reservation) {
	e.mu.Lock()
	e.inflight++
	e.mu.Unlock()
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() { <-e.r.slots }()
		defer func() {
			e.mu.Lock()
			e.inflight--
			e.mu.Unlock()
		}()
		defer res.release()
		defer func() {
			if p := recover(); p != nil {
				slog.Error("jev replay: call panicked; it is asked again at the next pass", "decision_id", d.ID, "arm", arm.ID, "panic", p)
			}
		}()
		e.call(ctx, arm, d)
	}()
}

// call sends the saved input of d to arm, as stored, and records the outcome as
// one result row (an answer or a failure). A call cut off by a shutdown leaves no
// row and counts nothing.
func (e *execution) call(ctx context.Context, arm jevtrialsettings.Arm, d repo.SavedDecision) {
	r := e.r
	cctx, cancel := context.WithTimeout(ctx, armCallTimeout)
	defer cancel()
	resp, latency, requestBytes, err := e.callers[arm.ID].TraderRaw(cctx, arm.PromptVersion, json.RawMessage(d.StateJSON))
	if err != nil && ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return // shutdown, not a failure of the arm
	}
	end := r.now()
	dec := trial.BuildDecision(arm, d.ID, len(d.StateJSON), trial.CallOutcome{Response: resp, Latency: latency, RequestBytes: requestBytes, Err: err}, end, r.secrets)
	_, known := arm.TargetableFrom()
	dec.LLMCutoffStatus = cutoffStatus(arm)
	row := jevreplay.Result{RunID: e.run.ID, Decision: dec, ReferenceOnly: !known}
	rctx, rcancel := r.recordCtx()
	defer rcancel()
	insertErr := r.store.InsertResult(rctx, row)
	e.mu.Lock()
	e.spent += dec.CountedCostUSD // an unstored (possibly billed) call still counts against the run
	if insertErr != nil {
		e.abort = true
	}
	e.mu.Unlock()
	if insertErr != nil {
		slog.Error("jev replay: store the result", "decision_id", d.ID, "arm", arm.ID, "error", insertErr)
		return
	}
	if err != nil {
		e.afterFailure(arm, dec, end)
	}
}

// afterFailure applies the stop a failed row calls for (the trial's rules, held
// in memory and in this run: a replay never writes the trial's stop records):
// HTTP 402 ends the run (the shared key is out of credit), HTTP 403 stops the arm
// for the JST day, HTTP 429/529 stops the arm for the cooldown. Any other
// failure is only counted (FailedByArmBetween bounds an arm's failed calls per day).
func (e *execution) afterFailure(arm jevtrialsettings.Arm, d jevtrial.Decision, now time.Time) {
	status := 0
	if d.HTTPStatus != nil {
		status = *d.HTTPStatus
	}
	switch {
	case status == 402:
		e.setEnd(jevreplay.StatusStopped, jevreplay.StopPaymentRequired)
	case status == 403:
		e.r.holdHalt(arm.ID, jevshadowsettings.Day(now))
		e.blockArm(arm.ID, jevtrial.SkipStoppedForbidden)
	case jevtrialsettings.IsThrottleStatus(status):
		e.r.holdThrottle(arm.ID, now.Add(jevtrialsettings.ThrottleCooldown))
		e.blockArm(arm.ID, jevtrial.SkipThrottled)
	}
}
