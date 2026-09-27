package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// refreshCandidates recomputes screener.Run over every active
// instrument's latest snapshot and turnoverTrailingBars-bar trailing
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
	actives, err := s.Instruments.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap: list active instruments: %w", err)
	}

	inputs := make([]screener.Input, 0, len(actives))
	for _, inst := range actives {
		bars, err := s.Snapshots.ListByInstrument(ctx, inst.ID, turnoverTrailingBars)
		if err != nil {
			return fmt.Errorf("bootstrap: list snapshots for %q: %w", inst.Symbol, err)
		}
		if len(bars) == 0 {
			continue
		}

		var turnover5m float64
		for _, bar := range bars {
			turnover5m += bar.Turnover
		}

		inputs = append(inputs, screener.Input{
			InstrumentID:  inst.ID,
			Symbol:        inst.Symbol,
			Snapshot:      bars[0], // ListByInstrument orders most-recent-first
			Turnover5mJPY: turnover5m,
			// BreakoutStrength/VolatilityExpansion have no computation
			// source yet (neither Feature Engine's Feature struct nor any
			// other service in this build computes them); ScreenScore
			// already treats a nil term as "drop from the sum", not
			// "zero" (screener.go's own doc comment), so leaving them nil
			// here is correct rather than a placeholder.
		})
	}

	candidates := screener.Run(s.strategy.FastScreener, inputs)
	s.Screener.Set(candidates, time.Now().UTC())

	now := time.Now().UTC()
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
