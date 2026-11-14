package trial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// run is one arm's comparison. A skip (the prompt version is not registered, no
// key, a stop of the day, a cap, pacing) returns nil or an error that is only
// logged; a call that was made always ends as a stored row (success or failure),
// except a call cancelled by Close.
func (t *Trial) run(ctx context.Context, cfg jevtrialsettings.Settings, arm jevtrialsettings.Arm, live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) error {
	if !jev.IsTraderVersion(arm.PromptVersion) {
		return fmt.Errorf("jev trial: arm %q: prompt version %q is not a registered trader prompt version", arm.ID, arm.PromptVersion)
	}
	caller := t.callers(arm)
	if caller == nil || !caller.Configured() {
		slog.Debug("jev trial: the arm has no API key, skipping the comparison", "arm", arm.ID)
		return nil
	}
	reservedBytes, err := trialwire.RequestBytes(arm.Model, providerFor(arm), jev.TraderRequest{QuestionVersion: arm.PromptVersion, State: state, RAGContext: ragContext})
	if err != nil {
		return fmt.Errorf("jev trial: arm %q: size the request (not calling it): %w", arm.ID, err)
	}
	now := t.now()
	// The reservation holds this call against the caps until its row is stored.
	res, err := t.mayCall(ctx, cfg, arm, now, reservedUSD(reservedBytes))
	defer res.release()
	if res == nil || err != nil {
		return err
	}
	if !t.allowCall(arm.ID) {
		slog.Debug("jev trial: the arm was called less than the minimum interval ago, skipping the comparison", "decision_id", live.ID, "arm", arm.ID)
		return nil
	}

	in := armInput{live: live.ID, stateBytes: stateBytes(state), ragFlag: ragCarriesLiveAnswers(ragContext)}
	resp, latency, requestBytes, err := caller.Trader(ctx, jev.TraderRequest{QuestionVersion: arm.PromptVersion, State: state, RAGContext: ragContext})
	if err != nil && t.cancelledByClose(err) {
		// Shutdown, not a failure of the arm: no row, nothing counted.
		return nil
	}
	end := t.now()
	d := buildDecision(arm, in, callResult{resp: resp, latency: latency, requestBytes: requestBytes, err: err}, end, t.secrets)
	if err != nil {
		// Stop first (it limits further spending), then leave the row.
		t.stopAfterFailure(arm, d, end)
	}
	insertCtx, cancel := t.recordCtx()
	defer cancel()
	if _, insertErr := t.results.Insert(insertCtx, d); insertErr != nil {
		if err == nil {
			// An answered (possibly billed) call that could not be stored is
			// counted as a failure, so it still bounds the day.
			t.countFailure(arm.ID, jevshadowsettings.Day(end))
		}
		return fmt.Errorf("jev trial: persist the row of arm %q: %w", arm.ID, errors.Join(insertErr, err))
	}
	if err != nil {
		return fmt.Errorf("jev trial: arm %q call: %w", arm.ID, err)
	}
	return nil
}

// mayCall runs the checks that cost no request: a stop of the whole trial or
// of the arm for today, the arm's 429/529 cooldown, the arm's failed calls,
// then the overall and the arm's daily cost cap against the counted cost of
// the stored rows plus the reservations of the calls in flight and of this
// call (reserveUSD). It records the stop that a cap or the failure cap causes:
// only when the stored rows alone have reached the cap; a call held back only by
// reservations is skipped without a stop, since the calls in flight may still
// land under the cap.
//
// The returned reservation (nil: the arm must not be called) holds reserveUSD
// against the caps; the caller releases it once the call's row is stored. err
// only reports that a cost could not be read (then the arm is not called).
func (t *Trial) mayCall(ctx context.Context, cfg jevtrialsettings.Settings, arm jevtrialsettings.Arm, now time.Time, reserveUSD float64) (res *reservation, err error) {
	allowed := false
	day := jevshadowsettings.Day(now)
	st := jevtrialsettings.LoadArm(ctx, t.settings, arm.ID)
	if st.ReadErr != nil {
		// "No record" is not the safe side when the read failed: a stopped arm
		// would be called.
		return nil, fmt.Errorf("jev trial: read the stop records of arm %q (not calling it): %w", arm.ID, st.ReadErr)
	}
	if cfg.Halt.HaltOn(day) || st.Halt.HaltOn(day) || t.haltHeld(jevtrialsettings.KeyHalt, day) || t.haltHeld(jevtrialsettings.HaltKey(arm.ID), day) {
		return nil, nil
	}
	if st.Throttle.ActiveAt(now) || t.throttleHeld(arm.ID, now) {
		return nil, nil
	}
	// Reserve before reading the stored cost (see reserved), and give it back
	// unless the arm may be called.
	held := t.reserve(arm.ID, reserveUSD)
	defer func() {
		if err != nil || !allowed {
			held.release()
		}
	}()
	reservedTotal, reservedArm := t.reserved(arm.ID)
	from, to := jevshadowsettings.DayBounds(now)
	total, err := t.results.SpentBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("jev trial: read today's trial cost (not calling arm %q): %w", arm.ID, err)
	}
	if total.CountedCostUSD >= cfg.CapUSD {
		t.recordHalt(jevtrialsettings.KeyHalt, jevtrialsettings.Halt{
			Reason: jevtrialsettings.HaltReasonDailyCostCap, SpentUSD: total.CountedCostUSD, CapUSD: cfg.CapUSD,
		}, now)
		return nil, nil
	}
	spent, err := t.results.SpentByArmBetween(ctx, arm.ID, from, to)
	if err != nil {
		return nil, fmt.Errorf("jev trial: read today's cost of arm %q (not calling it): %w", arm.ID, err)
	}
	if spent.CountedCostUSD >= arm.CapUSD() {
		t.recordHalt(jevtrialsettings.HaltKey(arm.ID), jevtrialsettings.Halt{
			Reason: jevtrialsettings.HaltReasonArmCostCap, SpentUSD: spent.CountedCostUSD, CapUSD: arm.CapUSD(),
		}, now)
		return nil, nil
	}
	// The cost above only sums the rows that were stored; a call that could not
	// be stored is counted as a failure instead, and enough of them stop the
	// arm for the day too.
	if failed := st.Failures.FailuresOn(day); failed >= jevtrialsettings.DailyFailureCap {
		t.recordHalt(jevtrialsettings.HaltKey(arm.ID), jevtrialsettings.Halt{
			Reason: jevtrialsettings.HaltReasonDailyFailureCap, Failures: failed, FailureCap: jevtrialsettings.DailyFailureCap,
		}, now)
		return nil, nil
	}
	// The stored rows are under both caps; the calls in flight and this one, at
	// their reservations, must fit under them too. No stop is recorded: the calls in
	// flight may still land under the cap, and the next comparison asks again.
	if total.CountedCostUSD+reservedTotal > cfg.CapUSD || spent.CountedCostUSD+reservedArm > arm.CapUSD() {
		slog.Debug("jev trial: the calls in flight leave no room under the daily cap, skipping the comparison", "arm", arm.ID)
		return nil, nil
	}
	allowed = true
	return held, nil
}

// cancelledByClose reports whether err is the cancellation Close caused (not a
// timeout, which is context.DeadlineExceeded, nor an API failure).
func (t *Trial) cancelledByClose(err error) bool {
	return t.base.Err() != nil && errors.Is(err, context.Canceled)
}

// stateBytes is the size of the state as JSON (material for judging a
// suspected truncation); 0 if it cannot be encoded.
func stateBytes(state jev.ScoutState) int {
	b, err := json.Marshal(state)
	if err != nil {
		return 0
	}
	return len(b)
}
