package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

const (
	// shadowMaxInFlight bounds the shadow calls running at once. A full queue
	// drops the new comparison (logged) instead of waiting: the live decision
	// must never be slowed, and an unbounded backlog would also overshoot the
	// daily cost cap by every call already in flight when it is reached. With the
	// cap check before each call the overshoot is at most this many calls.
	// 4 is a user decision (確定).
	shadowMaxInFlight = 4
	// shadowCallTimeout bounds one shadow call including its retries (the
	// client's own bound for a fully failing call is 23.5s, evaluate.go).
	shadowCallTimeout = 45 * time.Second
)

// Results is the jev_shadow_decisions access Shadow needs;
// *judgement.ShadowRepository implements it.
type Results interface {
	Insert(ctx context.Context, d jevshadow.Decision) (jevshadow.Decision, error)
	SpentBetween(ctx context.Context, from, to time.Time) (judgement.Spend, error)
}

// Settings is the runtime_settings access Shadow needs: it reads the
// switch, the candidate and the cap, and records a day stopped by the cap.
// *system.RuntimeSettingsRepository implements it.
type Settings interface {
	jevshadowsettings.Store
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
}

// Shadow asks a candidate Trader prompt version about the same input as a live
// decision and stores the answer in jev_shadow_decisions (issue #872). The
// answer is never used: Shadow has no path to Policy, the meta-model, Risk,
// order placement, the RAG index (jev_decision_vectors), Calibration or the
// meta-model training. It only writes jev_shadow_decisions and a few
// runtime_settings rows (the failed-call counter, why it stopped for a day by
// a cap, and the 429/529 stop record).
//
// The Jev API key and its rate limit are shared with the live Scout/Trader
// (issue #883), so the shadow gives way to them: it sends at most one call per
// jevshadowsettings.MinCallInterval (a comparison that would start sooner is
// dropped), never retries after a 429/529 and stays stopped for
// jevshadowsettings.ThrottleCooldown after one, whether its own call or a live
// call got it. The stop and its reason are stored (jevshadowsettings.KeyThrottle)
// for the Settings screen and jev_shadow_cost.
//
// It runs only in Paper (metalabelsettings.RuntimeMode: NewShadow takes the
// mode, so a test can prove Live does nothing), only while the switch is on,
// and never slows or fails the live decision: Submit returns immediately, the
// call runs in its own goroutine under its own timeout, and every failure is a
// log line.
type Shadow struct {
	client   *jev.Client
	results  Results
	settings Settings
	mode     metalabelsettings.Mode
	now      func() time.Time
	slots    chan struct{}
	wg       sync.WaitGroup
	// base is the parent of every call's context; Close cancels it so a
	// shutdown does not wait out the calls' timeouts.
	base   context.Context
	cancel context.CancelFunc
	// failMu serializes the read-modify-write of the failed-call counter and
	// throttleMu that of the throttle record.
	failMu     sync.Mutex
	throttleMu sync.Mutex
	// minInterval is the least time between two calls (0: no pacing), measured
	// by pace (time.Now in production) from lastCall under paceMu.
	minInterval time.Duration
	pace        func() time.Time
	paceMu      sync.Mutex
	lastCall    time.Time
	// throttledUntil is the end of the cooldown after a 429/529, kept in memory
	// (under paceMu) so it holds when the throttle record cannot be written.
	throttledUntil time.Time
}

// New returns a Shadow that calls client, stores answers via results and
// reads/records its settings via settings. mode is the operating mode of this
// build (metalabelsettings.RuntimeMode): the shadow does nothing unless it is
// metalabelsettings.ModePaper. now is the clock (time.Now in production).
func New(client *jev.Client, results Results, settings Settings, mode metalabelsettings.Mode, now func() time.Time, opts ...Option) *Shadow {
	base, cancel := context.WithCancel(context.Background())
	s := &Shadow{client: client, results: results, settings: settings, mode: mode, now: now, slots: make(chan struct{}, shadowMaxInFlight), base: base, cancel: cancel,
		minInterval: jevshadowsettings.MinCallInterval, pace: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Submit hands live (a persisted trader decision) and the exact TraderState and
// RAG context it was decided with to the shadow, and returns at once. It does
// nothing outside Paper, for a nil Shadow, or when the queue is full.
func (s *Shadow) Submit(live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) {
	if s == nil || s.mode != metalabelsettings.ModePaper || s.base.Err() != nil {
		return
	}
	select {
	case s.slots <- struct{}{}:
	default:
		slog.Warn("jev shadow: queue full, skipping the comparison", "decision_id", live.ID, "symbol", live.Symbol)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.slots }()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("jev shadow: shadow panicked; the live decision is unaffected", "decision_id", live.ID, "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(s.base, shadowCallTimeout)
		defer cancel()
		if err := s.run(ctx, live, state, ragContext); err != nil {
			slog.Warn("jev shadow: comparison failed; the live decision is unaffected", "decision_id", live.ID, "symbol", live.Symbol, "error", err)
		}
	}()
}

// Wait blocks until every submitted comparison has finished (tests).
func (s *Shadow) Wait() { s.wg.Wait() }

// Close is the shutdown: it cancels the calls in flight (the application's
// stop, bootstrap.Services.Stop, calls it through jev.Trader.Close) and waits for
// them to return. Comparisons submitted afterwards are dropped. A nil Shadow
// is a no-op.
func (s *Shadow) Close() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

// run is one comparison. A skip (switch off, no candidate, candidate equal to
// the live version, cap reached) returns nil; a failure returns an error.
func (s *Shadow) run(ctx context.Context, live domain.JevDecision, state jev.ScoutState, ragContext rag.Context) error {
	cfg := jevshadowsettings.Load(ctx, s.settings)
	candidate := cfg.CandidateVersion
	if !cfg.Enabled || candidate == "" || candidate == live.QuestionVersion {
		return nil
	}
	if !jev.IsTraderVersion(candidate) {
		return fmt.Errorf("jev shadow: the shadow candidate %q is not a registered trader prompt version", candidate)
	}

	now := s.now()
	day := jevshadowsettings.Day(now)
	// The Jev API is throttling (429/529): stay off it, for the live calls'
	// sake. Neither check costs a request.
	if cfg.Throttle.ActiveAt(now) || s.throttleHeld(now) {
		return nil
	}
	if ev, ok := s.client.LastThrottle(); ok && now.Sub(ev.At) < jevshadowsettings.ThrottleCooldown {
		s.recordThrottle(ctx, cfg, ev.Status, jevshadowsettings.ThrottleSourceLive, ev.At)
		return nil
	}
	from, to := jevshadowsettings.DayBounds(now)
	spent, err := s.results.SpentBetween(ctx, from, to)
	if err != nil {
		return fmt.Errorf("jev shadow: read today's shadow cost (not calling Jev): %w", err)
	}
	if spent.CostUSD >= cfg.CapUSD {
		s.recordHalt(ctx, cfg, now, jevshadowsettings.Halt{
			Reason: jevshadowsettings.HaltReasonDailyCostCap, SpentUSD: spent.CostUSD, CapUSD: cfg.CapUSD,
		})
		return nil
	}
	// The cost above only sums the calls that were stored. A call the vendor
	// may have billed but that was never stored (invalid response, failed
	// insert, timeout) is counted as a failure instead, and enough of them stop
	// the day too (DailyFailureCap).
	if failed := cfg.Failures.FailuresOn(day); failed >= jevshadowsettings.DailyFailureCap {
		s.recordHalt(ctx, cfg, now, jevshadowsettings.Halt{
			Reason: jevshadowsettings.HaltReasonDailyFailureCap, SpentUSD: spent.CostUSD, CapUSD: cfg.CapUSD,
			Failures: failed, FailureCap: jevshadowsettings.DailyFailureCap,
		})
		return nil
	}

	if !s.allowCall() {
		slog.Debug("jev shadow: a call was made less than the minimum interval ago, skipping the comparison", "decision_id", live.ID)
		return nil
	}

	resp, latency, err := s.client.ShadowTrader(ctx, jev.TraderRequest{QuestionVersion: candidate, State: state, RAGContext: ragContext})
	if err != nil {
		// A 429/529 is the API saying it is busy, not a failed call: stop for the
		// cooldown (nothing was answered, so nothing is billed or counted
		// against DailyFailureCap).
		var apiErr *jev.APIError
		if errors.As(err, &apiErr) && jevshadowsettings.ThrottleReason(apiErr.StatusCode) != "" {
			s.recordThrottle(ctx, cfg, apiErr.StatusCode, jevshadowsettings.ThrottleSourceShadow, s.now())
			return fmt.Errorf("jev shadow: the Jev API answered %d; stopped for %s: %w", apiErr.StatusCode, jevshadowsettings.ThrottleCooldown, err)
		}
		s.countFailure(day)
		return fmt.Errorf("jev shadow: shadow call for version %q: %w", candidate, err)
	}
	responseJSON, err := json.Marshal(resp)
	if err != nil {
		s.countFailure(day)
		return fmt.Errorf("jev shadow: encode shadow response: %w", err)
	}
	// A response without usage must not count as 0 USD (that would let it
	// slip under the daily cap): its input tokens are estimated from the
	// request's size (EstimateInputTokens) and the row is marked as an
	// estimate.
	tokens, estimated := resp.InputTokens, false
	if tokens <= 0 {
		tokens, estimated = EstimateInputTokens(resp.RequestBytes), true
		slog.Warn("jev shadow: the response carried no usage; counting the call by an estimate of its input tokens",
			"decision_id", live.ID, "request_bytes", resp.RequestBytes, "estimated_tokens", tokens)
	}
	if _, err := s.results.Insert(ctx, jevshadow.Decision{
		JevDecisionID:           live.ID,
		PromptVersion:           candidate,
		ModelID:                 resp.ModelID,
		ResponseJSON:            string(responseJSON),
		Direction:               resp.Direction,
		Regime:                  resp.Regime,
		EntryQuality:            resp.EntryQuality,
		Confidence:              resp.Confidence,
		ToxicFlow:               resp.ToxicFlow,
		LiquidityStressed:       resp.LiquidityStressed,
		ContinuationProbability: resp.ContinuationProbability,
		LatencyMs:               int(latency.Milliseconds()),
		InputTokens:             tokens,
		RequestCost:             InputCostUSD(tokens),
		CostIsEstimate:          estimated,
		CreatedAt:               s.now().UTC(),
	}); err != nil {
		s.countFailure(day)
		return fmt.Errorf("jev shadow: persist shadow decision: %w", err)
	}
	return nil
}

// countFailure adds one failed call to the stored counter of the JST day day.
// It writes with its own short context: the call's may be the one that timed
// out or was cancelled. A failed write is logged; the next failure counts on.
func (s *Shadow) countFailure(day string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.base), 5*time.Second)
	defer cancel()
	s.failMu.Lock()
	defer s.failMu.Unlock()
	f := jevshadowsettings.Failures{Date: day}
	if stored := jevshadowsettings.Load(ctx, s.settings).Failures; stored != nil && stored.Date == day {
		f.Count = stored.Count
	}
	f.Count++
	value, err := jevshadowsettings.EncodeFailures(f)
	if err == nil {
		err = s.settings.Set(ctx, jevshadowsettings.KeyFailures, value, s.now())
	}
	if err != nil {
		slog.Error("jev shadow: count the shadow's failed call", "error", err)
	}
}

// recordHalt leaves the reason the shadow stopped today (h: the daily cost cap
// or the daily failure cap with its numbers), once per day and reason, so the
// GUI and the MCP can show why. A failed write is logged; the shadow stays
// stopped either way because both caps are checked against stored state before
// every call.
func (s *Shadow) recordHalt(ctx context.Context, cfg jevshadowsettings.Settings, now time.Time, h jevshadowsettings.Halt) {
	h.Date, h.At = jevshadowsettings.Day(now), now.UTC().Format(time.RFC3339)
	if p := cfg.Halt; p != nil && p.Date == h.Date && p.Reason == h.Reason && p.CapUSD == h.CapUSD && p.FailureCap == h.FailureCap {
		return
	}
	value, err := jevshadowsettings.EncodeHalt(h)
	if err == nil {
		err = s.settings.Set(ctx, jevshadowsettings.KeyHalt, value, now)
	}
	if err != nil {
		slog.Error("jev shadow: record why the shadow stopped for the day", "reason", h.Reason, "error", err)
		return
	}
	slog.Warn("jev shadow: stopped for the day; the live decisions continue",
		"date", h.Date, "reason", h.Reason, "spent_usd", h.SpentUSD, "cap_usd", h.CapUSD, "failures", h.Failures)
}
