package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevshadow"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

const (
	// shadowMaxInFlight bounds the shadow calls running at once. A full queue
	// drops the new comparison (logged) instead of waiting: the live decision
	// must never be slowed, and an unbounded backlog would also overshoot the
	// daily cost cap by every call already in flight when it is reached. With the
	// cap check before each call the overshoot is at most this many calls.
	shadowMaxInFlight = 4
	// shadowCallTimeout bounds one shadow call including its retries (the
	// client's own bound for a fully failing call is 23.5s, evaluate.go).
	shadowCallTimeout = 45 * time.Second
)

// ShadowResults is the jev_shadow_decisions access Shadow needs;
// *judgement.ShadowRepository implements it.
type ShadowResults interface {
	Insert(ctx context.Context, d jevshadow.Decision) (jevshadow.Decision, error)
	SpentBetween(ctx context.Context, from, to time.Time) (judgement.Spend, error)
}

// ShadowSettings is the runtime_settings access Shadow needs: it reads the
// switch, the candidate and the cap, and records a day stopped by the cap.
// *system.RuntimeSettingsRepository implements it.
type ShadowSettings interface {
	jevshadowsettings.Store
	Set(ctx context.Context, key, value string, updatedAt time.Time) error
}

// Shadow asks a candidate Trader prompt version about the same input as a live
// decision and stores the answer in jev_shadow_decisions (issue #872). The
// answer is never used: Shadow has no path to Policy, the meta-model, Risk,
// order placement, the RAG index (jev_decision_vectors), Calibration or the
// meta-model training. It only writes jev_shadow_decisions and (for a day
// stopped by the cost cap) one runtime_settings row.
//
// It runs only in Paper (metalabelsettings.RuntimeMode: NewShadow takes the
// mode, so a test can prove Live does nothing), only while the switch is on,
// and never slows or fails the live decision: Submit returns immediately, the
// call runs in its own goroutine under its own timeout, and every failure is a
// log line.
type Shadow struct {
	client   *Client
	results  ShadowResults
	settings ShadowSettings
	mode     metalabelsettings.Mode
	now      func() time.Time
	slots    chan struct{}
	wg       sync.WaitGroup
}

// NewShadow returns a Shadow that calls client, stores answers via results and
// reads/records its settings via settings. mode is the operating mode of this
// build (metalabelsettings.RuntimeMode): the shadow does nothing unless it is
// metalabelsettings.ModePaper. now is the clock (time.Now in production).
func NewShadow(client *Client, results ShadowResults, settings ShadowSettings, mode metalabelsettings.Mode, now func() time.Time) *Shadow {
	return &Shadow{client: client, results: results, settings: settings, mode: mode, now: now, slots: make(chan struct{}, shadowMaxInFlight)}
}

// Submit hands live (a persisted trader decision) and the exact TraderState and
// RAG context it was decided with to the shadow, and returns at once. It does
// nothing outside Paper, for a nil Shadow, or when the queue is full.
func (s *Shadow) Submit(live domain.JevDecision, state ScoutState, ragContext rag.Context) {
	if s == nil || s.mode != metalabelsettings.ModePaper {
		return
	}
	select {
	case s.slots <- struct{}{}:
	default:
		slog.Warn("jev: shadow queue full, skipping the comparison", "decision_id", live.ID, "symbol", live.Symbol)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.slots }()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("jev: shadow panicked; the live decision is unaffected", "decision_id", live.ID, "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), shadowCallTimeout)
		defer cancel()
		if err := s.run(ctx, live, state, ragContext); err != nil {
			slog.Warn("jev: shadow comparison failed; the live decision is unaffected", "decision_id", live.ID, "symbol", live.Symbol, "error", err)
		}
	}()
}

// Wait blocks until every submitted comparison has finished (tests and a clean
// shutdown).
func (s *Shadow) Wait() { s.wg.Wait() }

// run is one comparison. A skip (switch off, no candidate, candidate equal to
// the live version, cap reached) returns nil; a failure returns an error.
func (s *Shadow) run(ctx context.Context, live domain.JevDecision, state ScoutState, ragContext rag.Context) error {
	cfg := jevshadowsettings.Load(ctx, s.settings)
	candidate := cfg.CandidateVersion
	if !cfg.Enabled || candidate == "" || candidate == live.QuestionVersion {
		return nil
	}
	if !IsTraderVersion(candidate) {
		return fmt.Errorf("jev: the shadow candidate %q is not a registered trader prompt version", candidate)
	}

	now := s.now()
	from, to := jevshadowsettings.DayBounds(now)
	spent, err := s.results.SpentBetween(ctx, from, to)
	if err != nil {
		return fmt.Errorf("jev: read today's shadow cost (not calling Jev): %w", err)
	}
	if spent.CostUSD >= cfg.CapUSD {
		s.recordHalt(ctx, cfg, now, spent.CostUSD)
		return nil
	}

	resp, latency, err := s.client.ShadowTrader(ctx, TraderRequest{QuestionVersion: candidate, State: state, RAGContext: ragContext})
	if err != nil {
		return fmt.Errorf("jev: shadow call for version %q: %w", candidate, err)
	}
	responseJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("jev: encode shadow response: %w", err)
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
		InputTokens:             resp.InputTokens,
		RequestCost:             InputCostUSD(resp.InputTokens),
		CreatedAt:               s.now().UTC(),
	}); err != nil {
		return fmt.Errorf("jev: persist shadow decision: %w", err)
	}
	return nil
}

// recordHalt leaves the reason the shadow stopped today (the daily cost cap,
// the spend and the cap), once per day and cap value, so the GUI and the MCP
// can show why. A failed write is logged; the shadow stays stopped either way
// because the cap is checked against the stored cost before every call.
func (s *Shadow) recordHalt(ctx context.Context, cfg jevshadowsettings.Settings, now time.Time, spentUSD float64) {
	today := jevshadowsettings.Day(now)
	if h := cfg.Halt; h != nil && h.Date == today && h.CapUSD == cfg.CapUSD {
		return
	}
	value, err := jevshadowsettings.EncodeHalt(jevshadowsettings.Halt{
		Date: today, Reason: jevshadowsettings.HaltReasonDailyCostCap, SpentUSD: spentUSD, CapUSD: cfg.CapUSD, At: now.UTC().Format(time.RFC3339),
	})
	if err == nil {
		err = s.settings.Set(ctx, jevshadowsettings.KeyHalt, value, now)
	}
	if err != nil {
		slog.Error("jev: record the shadow's daily cost cap stop", "error", err)
		return
	}
	slog.Warn("jev: shadow stopped for the day: the daily cost cap was reached; the live decisions continue",
		"date", today, "spent_usd", spentUSD, "cap_usd", cfg.CapUSD)
}
