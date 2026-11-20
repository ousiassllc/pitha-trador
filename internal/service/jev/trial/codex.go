package trial

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/codexwire"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// codexArmTimeout bounds one Codex arm: the CLI run (codexwire.DefaultCallTimeout)
// plus the checks and the write of its row.
const codexArmTimeout = codexwire.DefaultCallTimeout + 30*time.Second

// The ChatGPT subscription (Codex CLI) provider, issue #1081. It differs from the
// OpenRouter and 独自 API arms on purpose:
//   - Cost is not USD. A call is stored as cost_source=subscription with no
//     cost_usd and counted 0 in the USD caps (SubscriptionCost); it is bounded by
//     the provider's daily call and token caps instead.
//   - One call at a time for the whole provider (every arm shares one ChatGPT
//     account and one auth file): the arms of one live decision run one after the
//     other, and a decision that finds the provider still busy skips its Codex arms
//     (provider_busy) instead of queueing.
//   - A failed call stops the whole provider, never a retry. Until when: the reset
//     time of a usage-limit message when the CLI gave one, otherwise the end of the
//     JST day (the 15-minute cooldown of a 429/529 is not used: a usage limit lasts
//     hours). Only a text that matches a usage-limit message is called
//     "usage_limit"; every other failure is "failed".
//   - The whole-trial USD stops (the overall cost cap, an OpenRouter 402) do not
//     stop it: they are about USD.

// runCodexArms runs arms (the Codex arms of one live decision) one after the other
// in this goroutine, which holds one of the trial's slots.
func (t *Trial) runCodexArms(cfg jevtrialsettings.Settings, arms []jevtrialsettings.Arm, live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) {
	defer t.wg.Done()
	defer func() { <-t.slots }()
	select {
	case t.codexBusy <- struct{}{}:
		defer func() { <-t.codexBusy }()
	default:
		for _, arm := range arms {
			t.recordSkip(live.ID, arm.ID, jevtrial.SkipProviderBusy)
		}
		return
	}
	for _, arm := range arms {
		if t.base.Err() != nil {
			return
		}
		t.runCodexArm(cfg, arm, live, state, ragContext)
	}
}

func (t *Trial) runCodexArm(cfg jevtrialsettings.Settings, arm jevtrialsettings.Arm, live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("jev trial: codex arm panicked; the live decision is unaffected", "decision_id", live.ID, "arm", arm.ID, "panic", r)
		}
	}()
	ctx, cancel := context.WithTimeout(t.base, codexArmTimeout)
	defer cancel()
	if err := t.codexRun(ctx, cfg, arm, live, state, ragContext); err != nil {
		slog.Warn("jev trial: codex comparison failed; the live decision is unaffected", "decision_id", live.ID, "symbol", live.Symbol, "arm", arm.ID, "error", err)
	}
}

// codexRun is one Codex arm's comparison: the checks that cost no call (the
// switch, the stop, the login, the caps, pacing), then one `codex exec`, then its
// row. A skip leaves a jev_trial_skips record.
func (t *Trial) codexRun(ctx context.Context, cfg jevtrialsettings.Settings, arm jevtrialsettings.Arm, live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) error {
	if !jev.IsTraderVersion(arm.PromptVersion) {
		t.recordSkip(live.ID, arm.ID, jevtrial.SkipPromptUnregistered)
		return fmt.Errorf("jev trial: arm %q: prompt version %q is not a registered trader prompt version", arm.ID, arm.PromptVersion)
	}
	caller, _ := t.callers(arm).(SubscriptionCaller)
	if caller == nil || !caller.Configured() {
		t.recordSkip(live.ID, arm.ID, jevtrial.SkipKeyNotSet) // no dedicated CODEX_HOME
		return nil
	}
	if !cfg.Codex.Enabled {
		t.recordSkip(live.ID, arm.ID, jevtrial.SkipProviderDisabled)
		return nil
	}
	now := t.now()
	skip, err := t.mayCallCodex(ctx, cfg, arm, caller, now)
	if err != nil {
		t.recordSkip(live.ID, arm.ID, jevtrial.SkipCheckFailed)
		return err
	}
	if skip != "" {
		t.recordSkip(live.ID, arm.ID, skip)
		return nil
	}
	if !t.allowCall(arm.ID, jevtrialsettings.CodexMinCallInterval) {
		t.recordSkip(live.ID, arm.ID, jevtrial.SkipMinInterval)
		return nil
	}

	req := jev.TraderRequest{QuestionVersion: arm.PromptVersion, State: state, RAGContext: ragContext}
	in := armInput{live: live.ID, stateBytes: stateBytes(state), ragFlag: ragCarriesLiveAnswers(ragContext)}
	resp, latency, requestBytes, err := caller.Trader(ctx, req)
	if err != nil && t.cancelledByClose(err) {
		return nil // shutdown, not a failure of the provider
	}
	end := t.now()
	d := buildDecision(arm, in, callResult{resp: resp, latency: latency, requestBytes: requestBytes, err: err}, end, t.secrets)
	if err != nil {
		t.stopCodexAfterFailure(arm, err, end)
	}
	insertCtx, cancel := t.recordCtx()
	defer cancel()
	if _, insertErr := t.results.Insert(insertCtx, d); insertErr != nil {
		if err == nil {
			t.countFailure(arm.ID, jevshadowsettings.Day(end))
		}
		return fmt.Errorf("jev trial: persist the row of arm %q: %w", arm.ID, errors.Join(insertErr, err))
	}
	if err != nil {
		return fmt.Errorf("jev trial: arm %q codex call: %w", arm.ID, err)
	}
	return nil
}

// mayCallCodex runs the checks that cost no call. skip is the reason the arm is
// not called ("" to call it); err only reports that something could not be read
// (then the arm is not called).
func (t *Trial) mayCallCodex(ctx context.Context, cfg jevtrialsettings.Settings, arm jevtrialsettings.Arm, caller SubscriptionCaller, now time.Time) (skip string, err error) {
	if h := cfg.Codex.Halt; h.ActiveAt(now) {
		return codexHaltSkip(h.Reason), nil
	}
	if reason, held := t.codexHeld(now); held {
		return codexHaltSkip(reason), nil
	}
	st := jevtrialsettings.LoadArm(ctx, t.settings, arm.ID)
	if st.ReadErr != nil {
		return "", fmt.Errorf("jev trial: read the failed-call counter of arm %q (not calling it): %w", arm.ID, st.ReadErr)
	}
	day := jevshadowsettings.Day(now)
	if st.Halt.HaltOn(day) {
		return haltSkip(st.Halt.Reason), nil
	}
	if st.Failures.FailuresOn(day) >= jevtrialsettings.DailyFailureCap {
		t.recordHalt(jevtrialsettings.HaltKey(arm.ID), jevtrialsettings.Halt{
			Reason: jevtrialsettings.HaltReasonDailyFailureCap, Failures: st.Failures.FailuresOn(day), FailureCap: jevtrialsettings.DailyFailureCap,
		}, now)
		return jevtrial.SkipStoppedFailureCap, nil
	}
	if loginState, _ := t.checkCodexLogin(ctx, caller, cfg.Codex.Login, now); loginState != jevtrialsettings.CodexLoginChatGPT {
		return jevtrial.SkipNotLoggedIn, nil
	}
	from, to := jevshadowsettings.DayBounds(now)
	// The provider runs one call at a time and every row is stored before the next
	// call, so the stored rows are the whole count: no reservation is needed. The
	// call that reaches a cap is the last one of the day (it can overshoot by one
	// call: its tokens are not known before it runs).
	usage, err := t.results.UsageByProviderBetween(ctx, jevtrialsettings.ProviderCodexChatGPT, from, to)
	if err != nil {
		return "", fmt.Errorf("jev trial: read today's Codex usage (not calling arm %q): %w", arm.ID, err)
	}
	switch {
	case usage.Calls >= cfg.Codex.CallCap:
		t.haltCodex(jevtrialsettings.CodexHalt{Reason: jevtrialsettings.CodexHaltCallCap, Calls: usage.Calls, Tokens: usage.Tokens}, endOfDay(now), false, now)
		return jevtrial.SkipDailyCapCalls, nil
	case usage.Tokens >= cfg.Codex.TokenCap:
		t.haltCodex(jevtrialsettings.CodexHalt{Reason: jevtrialsettings.CodexHaltTokenCap, Calls: usage.Calls, Tokens: usage.Tokens}, endOfDay(now), false, now)
		return jevtrial.SkipDailyCapTokens, nil
	}
	return "", nil
}

func codexHaltSkip(reason string) string {
	switch reason {
	case jevtrialsettings.CodexHaltUsageLimit:
		return jevtrial.SkipStoppedUsageLimit
	case jevtrialsettings.CodexHaltCallCap:
		return jevtrial.SkipDailyCapCalls
	case jevtrialsettings.CodexHaltTokenCap:
		return jevtrial.SkipDailyCapTokens
	}
	return jevtrial.SkipStoppedProviderFailed
}

// endOfDay is the start of the next JST day after now: when "until the end of the
// day" stops end.
func endOfDay(now time.Time) time.Time {
	_, to := jevshadowsettings.DayBounds(now)
	return to
}

// stopCodexAfterFailure applies the stop a failed Codex call calls for. A run
// that failed (*codexwire.CallError, or a timeout) stops the whole provider; an
// answer that is not a valid answer set only adds a failed call to the arm's day
// counter (the CLI itself worked).
func (t *Trial) stopCodexAfterFailure(arm jevtrialsettings.Arm, err error, now time.Time) {
	var callErr *codexwire.CallError
	isCall := errors.As(err, &callErr)
	if errors.Is(err, systemone.ErrInvalidResponse) && !isCall {
		t.countFailure(arm.ID, jevshadowsettings.Day(now))
		return
	}
	h := jevtrialsettings.CodexHalt{Reason: jevtrialsettings.CodexHaltFailed}
	until, known := endOfDay(now), false
	if callErr != nil && callErr.UsageLimit {
		h.Reason = jevtrialsettings.CodexHaltUsageLimit
		if !callErr.Until.IsZero() {
			until, known = callErr.Until, true
		}
	}
	t.haltCodex(h, until, known, now)
}

// haltCodex stops the Codex provider until until (known: the CLI gave the time).
// It holds in memory and is recorded (the first, longest stop stays); a failed
// write is logged and the stop still holds.
func (t *Trial) haltCodex(h jevtrialsettings.CodexHalt, until time.Time, known bool, now time.Time) {
	t.holdCodex(until, h.Reason)
	ctx, cancel := t.recordCtx()
	defer cancel()
	h.At, h.Until, h.UntilKnown = now.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339), known
	t.recMu.Lock()
	defer t.recMu.Unlock()
	if raw, ok, err := t.settings.Get(ctx, jevtrialsettings.KeyCodexHalt); err == nil && ok {
		if cur, perr := jevtrialsettings.ParseCodexHalt(raw); perr == nil && cur.ActiveAt(now) {
			if curUntil, _ := time.Parse(time.RFC3339, cur.Until); !curUntil.Before(until) {
				return
			}
		}
	}
	value, err := jevtrialsettings.EncodeCodexHalt(h)
	if err == nil {
		err = t.settings.Set(ctx, jevtrialsettings.KeyCodexHalt, value, now)
	}
	if err != nil {
		slog.Error("jev trial: record why the Codex provider stopped", "reason", h.Reason, "error", err)
		return
	}
	slog.Warn("jev trial: the Codex provider is stopped; no call or retry until then, the live decisions continue",
		"reason", h.Reason, "until", h.Until, "until_known", known)
}

// holdCodex extends the in-memory stop to until (never shortens it) and keeps its
// reason.
func (t *Trial) holdCodex(until time.Time, reason string) {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	if until.After(t.codexUntil) {
		t.codexUntil, t.codexReason = until, reason
	}
}

// codexHeld reports whether the in-memory stop is still running at now, and why.
func (t *Trial) codexHeld(now time.Time) (reason string, held bool) {
	t.paceMu.Lock()
	defer t.paceMu.Unlock()
	return t.codexReason, now.Before(t.codexUntil)
}

// checkCodexLogin asks the CLI's login state and records it (the screen and the
// MCP read it from the database) when it is a new check.
func (t *Trial) checkCodexLogin(ctx context.Context, caller SubscriptionCaller, stored *jevtrialsettings.CodexLogin, now time.Time) (string, time.Time) {
	state, at := caller.Login(ctx)
	if state == "" || !jevtrialsettings.ValidCodexLoginState(state) {
		return jevtrialsettings.CodexLoginUnavailable, now
	}
	rec := jevtrialsettings.CodexLogin{State: state, At: at.UTC().Format(time.RFC3339)}
	if stored != nil && *stored == rec {
		return state, at
	}
	rctx, cancel := t.recordCtx()
	defer cancel()
	value, err := jevtrialsettings.EncodeCodexLogin(rec)
	if err == nil {
		err = t.settings.Set(rctx, jevtrialsettings.KeyCodexLogin, value, now)
	}
	if err != nil {
		slog.Warn("jev trial: record the Codex login state", "error", err)
	}
	return state, at
}
