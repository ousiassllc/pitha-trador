package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// refreshCandidates recomputes screener.Run over every active
// instrument's latest snapshot and trailing 5-minute
// turnover, publishes the result to s.Screener (issue #45) for the
// Scanner Dashboard (internal/router.WithCandidateSource) to read, and -
// issue #46 - enqueues one jev-scout job per resulting candidate
// (functional.md §2's main flow: "FS->>JS: 候補銘柄（50〜200）", every
// scan cycle, not merely on first sight of a symbol - Jev Scout's own
// FR-SCOUT-1〜3 re-evaluates every still-passing candidate each cycle).
//
// An instrument with no market_snapshots rows yet (Feature Engine has not
// completed a cycle for it) is skipped rather than fabricating a
// zero-value snapshot input for it - the same "nil/absent over
// fabricated" precedent handleMarketData's own History/MarketReturn5m
// comment already follows.
func (s *Services) refreshCandidates(ctx context.Context) error {
	actives, err := s.Instruments.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil {
		return fmt.Errorf("bootstrap: list active instruments: %w", err)
	}

	cfg, err := s.fastScreenerConfig(ctx)
	if err != nil {
		return err
	}

	inputs := make([]screener.Input, 0, len(actives))
	for _, inst := range actives {
		// The latest bar plus featureengine.HistoryLookbackBars prior
		// bars: enough for both the trailing turnover and
		// ComputeScreenSignals' 15-minute volatility window.
		bars, err := s.Snapshots.ListByInstrument(ctx, inst.ID, featureengine.HistoryLookbackBars+1)
		if err != nil {
			return fmt.Errorf("bootstrap: list snapshots for %q: %w", inst.Symbol, err)
		}
		if len(bars) == 0 {
			continue
		}

		// Snapshot.Turnover is cumulative, so 5 minutes is a difference
		// (featureengine.TurnoverOverWindow), never a sum; unknown counts
		// as 0 and fails the liquidity floor (FR-FS-1).
		var turnover5m float64
		if t := featureengine.TurnoverOverWindow(bars[0].Timestamp, bars[0].Turnover, bars[1:], 5*time.Minute); t != nil {
			turnover5m = *t
		}

		// FR-FS-2's breakout_strength / volatility_expansion. A nil
		// signal (insufficient history) is dropped from screen_score
		// rather than scored as zero.
		signals := featureengine.ComputeScreenSignals(bars[0], bars[1:])

		inputs = append(inputs, screener.Input{
			InstrumentID:        inst.ID,
			Symbol:              inst.Symbol,
			Snapshot:            bars[0],
			Turnover5mJPY:       turnover5m,
			BreakoutStrength:    signals.BreakoutStrength,
			VolatilityExpansion: signals.VolatilityExpansion,
		})
	}

	candidates := screener.Run(cfg, inputs)
	s.Screener.Set(candidates, time.Now().UTC())

	now := time.Now().UTC()
	if !s.inSession(now) {
		// Jev Scout is billed per call: off-hours the candidate list still
		// refreshes from stored data, but nothing is sent to Jev
		// (non-functional.md §3).
		return nil
	}
	for _, c := range candidates {
		if err := s.enqueueJevScout(ctx, c.InstrumentID, c.Symbol, now); err != nil {
			// A single candidate's enqueue failure (DB write error) must
			// not drop the remaining candidates from this cycle's Jev
			// Scout pass - the same best-effort precedent
			// handleMarketData's own RAG-indexing comment follows for a
			// non-critical per-item side effect alongside a
			// already-committed primary result (here: the Screener.Set
			// above, which every candidate already reached regardless).
			slog.Error("bootstrap: enqueue jev-scout job failed", "instrument_id", c.InstrumentID, "symbol", c.Symbol, "error", err)
		}
	}

	return nil
}

// fastScreenerConfig returns the FR-FS-1/FR-FS-3 filter/weight settings
// for this cycle: s.strategy.FastScreener (config/strategy.yaml with
// PITHA_FAST_SCREENER_* env overrides already applied by
// config.LoadStrategy) overridden by every screener.* runtime_settings
// key currently in the DB. It is read per cycle so a DB change takes
// effect on the next refresh without a restart.
func (s *Services) fastScreenerConfig(ctx context.Context) (config.FastScreenerConfig, error) {
	cfg := s.strategy.FastScreener
	for _, key := range config.FastScreenerSettingKeys() {
		raw, ok, err := s.Settings.Get(ctx, key)
		if err != nil {
			return config.FastScreenerConfig{}, fmt.Errorf("bootstrap: read runtime setting %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := config.ApplyFastScreenerSetting(&cfg, key, raw); err != nil {
			return config.FastScreenerConfig{}, fmt.Errorf("bootstrap: %w", err)
		}
	}
	return cfg, nil
}

// enqueueJevScout enqueues one jev-scout queue job (jev.ScoutJobPayload)
// for instrumentID/symbol, due immediately at now.
func (s *Services) enqueueJevScout(ctx context.Context, instrumentID int64, symbol string, now time.Time) error {
	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: instrumentID, Symbol: symbol})
	if err != nil {
		return fmt.Errorf("bootstrap: encode jev-scout job payload for %q: %w", symbol, err)
	}
	if _, err := s.Jobs.Enqueue(ctx, repository.JobQueueJevScout, string(payload), now); err != nil {
		return fmt.Errorf("bootstrap: enqueue jev-scout job for %q: %w", symbol, err)
	}
	return nil
}

// candidateRefreshTicker runs refreshCandidates on config/strategy.yaml's
// scan.candidate_refresh_interval_seconds_min/max cadence (functional.md
// §4.3's 15-30s候補銘柄更新周期) until ctx is done. Scheduler.Start's own
// doc comment explicitly defers this cycle's trigger registration to "the
// Fast Screener... scope" (i.e. this one), since Scheduler has no generic
// way to add an arbitrary extra cron/ticker beyond the ones its own
// Start hardcodes (full-scan/self-improve/log-rotation).
func (s *Services) candidateRefreshTicker(ctx context.Context) {
	defer s.wg.Done()

	interval := candidateRefreshInterval{
		min: time.Duration(s.strategy.Scan.CandidateRefreshIntervalSecondsMin) * time.Second,
		max: time.Duration(s.strategy.Scan.CandidateRefreshIntervalSecondsMax) * time.Second,
	}

	for {
		timer := time.NewTimer(interval.next())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := s.refreshCandidates(ctx); err != nil {
				slog.Error("bootstrap: candidate refresh cycle failed", "error", err)
			}
		}
	}
}

// candidateRefreshInterval mirrors internal/web/handler.
// CandidateRefreshInterval's Min/Max-random-jitter behavior without
// importing internal/web/handler from this file (that import only
// becomes necessary at the cmd/ call site that also needs
// handler.CandidateRefreshInterval itself, to pass to
// router.WithCandidateRefreshInterval).
type candidateRefreshInterval struct {
	min, max time.Duration
}

func (r candidateRefreshInterval) next() time.Duration {
	if r.max <= r.min {
		return r.min
	}
	return r.min + time.Duration(rand.Int64N(int64(r.max-r.min)))
}
