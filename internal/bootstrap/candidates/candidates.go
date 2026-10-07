// Package candidates is the Fast Screener candidate-refresh cycle
// (functional.md §2/§4.2/§4.3, issues #45/#46): it recomputes the screened
// candidate list from the persisted snapshots, publishes it to the
// screener.LiveSource the Scanner Dashboard reads, and enqueues one
// jev-scout job per candidate. It lives under internal/bootstrap as
// composition-root glue; Refresher takes only the dependencies it uses
// (docs/architecture/overview.md §3).
package candidates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// Refresher runs the candidate-refresh cycle over the repositories and
// config it is given.
type Refresher struct {
	Instruments *market.InstrumentRepository
	Snapshots   *market.SnapshotRepository
	Settings    *system.RuntimeSettingsRepository
	Jobs        *jobqueue.JobRepository
	Screener    *screener.LiveSource
	// Decisions and Positions supply each candidate's latest Jev Trader
	// decision and open position for the Scanner Dashboard's Jev columns
	// (see attachJevState).
	Decisions *judgement.DecisionRepository
	Positions *trading.PositionRepository
	Strategy  *config.StrategyConfig
	Watch     Watch // ranking watch list restricting screening; nil screens all
	// InSession reports whether t is inside a trading session; a nil
	// InSession is always in session.
	InSession func(time.Time) bool
	// Now reports the current time; nil means time.Now. Tests inject a
	// fake clock to step through the per-symbol Scout cooldown.
	Now func() time.Time
}

func (r *Refresher) inSession(t time.Time) bool {
	return r.InSession == nil || r.InSession(t)
}

func (r *Refresher) now() time.Time {
	if r.Now == nil {
		return time.Now().UTC()
	}
	return r.Now().UTC()
}

// scoutHeld returns the instrument IDs a new jev-scout job must not be
// enqueued for at now (issue #388; non-functional.md §2.1 caps Jev calls
// at top_n × 2 per minute): those with a pending or running jev-scout job
// (whether from this cycle or from an FR-SCAN-1 event re-evaluation), and
// those whose last jev-scout job finished less than
// scan.jev_scout_min_interval_seconds ago. The 15-30s refresh cadence
// would otherwise re-run Scout for every candidate 2-4 times a minute.
func (r *Refresher) scoutHeld(ctx context.Context, now time.Time) (map[int64]bool, error) {
	cooldown := time.Duration(r.Strategy.Scan.JevScoutMinIntervalSeconds) * time.Second
	jobs, err := r.Jobs.ListOpenOrFinishedSince(ctx, jobqueue.JobQueueJevScout, now.Add(-cooldown))
	if err != nil {
		return nil, fmt.Errorf("candidates: list open jev-scout jobs: %w", err)
	}
	held := make(map[int64]bool, len(jobs))
	for _, job := range jobs {
		var payload jev.ScoutJobPayload
		if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
			slog.Warn("candidates: undecodable jev-scout job payload", "job_id", job.ID, "error", err)
			continue
		}
		held[payload.InstrumentID] = true
	}
	return held, nil
}

// Refresh recomputes screener.Run over every active
// instrument's latest snapshot and trailing 5-minute
// turnover, publishes the result to r.Screener (issue #45) for the
// Scanner Dashboard (internal/router.WithCandidateSource) to read, and -
// issue #46 - enqueues a jev-scout job per resulting candidate that has
// none pending/running and whose last Scout finished at least
// scan.jev_scout_min_interval_seconds ago (issue #388; see scoutHeld), so
// Jev Scout re-evaluates each still-passing candidate (FR-SCOUT-1〜3) at
// most once a minute rather than on every 15-30s cycle (functional.md §2's
// main flow: "FS->>JS: 候補銘柄（上位N件。既定 top_n=20…）").
//
// An instrument with no market_snapshots rows yet (Feature Engine has not
// completed a cycle for it) is skipped rather than fabricating a
// zero-value snapshot input for it - the same "nil/absent over
// fabricated" precedent marketdatajob's own History/MarketReturn5m
// comment already follows.
func (r *Refresher) Refresh(ctx context.Context) error {
	startedAt := r.now()
	actives, err := r.activeStocks(ctx)
	if err != nil {
		return fmt.Errorf("candidates: list active instruments: %w", err)
	}

	cfg, err := r.fastScreenerConfig(ctx)
	if err != nil {
		return err
	}

	// symbols is the Scanner Dashboard's per-symbol view of this cycle
	// (issue #303), one entry per active instrument in actives' (symbol)
	// order; symbols[inputSlot[i]] is inputs[i]'s entry.
	symbols := make([]domain.ScanSymbol, len(actives))
	inputSlot := make([]int, 0, len(actives))
	funnel := domain.ScanFunnel{Universe: len(actives)}
	inputs := make([]screener.Input, 0, len(actives))
	// The latest bar plus featureengine.HistoryLookbackBars prior bars per
	// instrument: enough for both the trailing turnover and
	// ComputeScreenSignals' 15-minute volatility window. Fetched in one
	// query without raw_data_json (issue #546) instead of a full-board
	// ListByInstrument per active stock every cycle.
	ids := make([]int64, len(actives))
	for i, inst := range actives {
		ids[i] = inst.ID
	}
	history, err := r.Snapshots.ListHistoryByInstruments(ctx, ids, featureengine.HistoryLookbackBars+1)
	if err != nil {
		return fmt.Errorf("candidates: list snapshot history: %w", err)
	}
	// A bar older than the mode's max age (domain.MaxSnapshotAge in
	// ranking-watch mode, scan.full_scan_max_snapshot_age_seconds in
	// full-scan mode, issue #686) is stale only during a session
	// (issue #685): off-session the retained watch list is shown from stored
	// data (#668), but once the session opens a bar kept from the previous
	// session / an earlier watch period must not reach Jev Scout until
	// market-data writes a fresh one.
	checkStale := r.inSession(startedAt)
	maxAge := r.Strategy.Scan.SnapshotMaxAge(domain.MaxSnapshotAge)
	for slot, inst := range actives {
		symbols[slot] = domain.ScanSymbol{InstrumentID: inst.ID, Symbol: inst.Symbol, Name: inst.Name, Market: inst.Market}
		bars := history[inst.ID]
		if len(bars) == 0 {
			symbols[slot].Reasons = symbols[slot].Reasons.Add(domain.ScreenReasonNoSnapshot)
			continue
		}
		funnel.FeatureComputed++

		// Snapshot.Turnover is cumulative, so 5 minutes is a difference
		// (featureengine.TurnoverOverWindow), never a sum; unknown counts
		// as 0 and fails the liquidity floor (FR-FS-1).
		var turnover5m float64
		t := featureengine.TurnoverOverWindow(bars[0].Timestamp, bars[0].Turnover, bars[1:], 5*time.Minute)
		if t != nil {
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
			TurnoverMissing:     t == nil,
			BreakoutStrength:    signals.BreakoutStrength,
			VolatilityExpansion: signals.VolatilityExpansion,
			Stale:               checkStale && bars[0].IsStale(startedAt, maxAge),
		})
		inputSlot = append(inputSlot, slot)
	}

	result := screener.Screen(cfg, inputs)
	candidates := result.Candidates
	for i, reasons := range result.Reasons {
		symbols[inputSlot[i]].Reasons = reasons
	}
	funnel.FastScreenerPassed = len(candidates)
	now := r.now()
	// Best-effort like the enqueue loop below: a failed read of the
	// decision/position tables must not withhold the refreshed candidate
	// list (or Jev Scout) - the Jev columns just fall back to nil
	// ("pending"/"flat") until the next cycle.
	if err := r.attachJevState(ctx, candidates); err != nil {
		slog.Error("candidates: attach jev state to candidates", "error", err)
	}
	r.Screener.Set(candidates, now)
	r.Screener.SetScan(domain.ScanCycle{StartedAt: startedAt, FinishedAt: now, Funnel: funnel, Symbols: symbols})

	if !r.inSession(now) {
		// Jev Scout is billed per call: off-hours the candidate list still
		// refreshes from stored data, but nothing is sent to Jev
		// (non-functional.md §3).
		return nil
	}
	held, err := r.scoutHeld(ctx, now)
	if err != nil {
		// Without the open/recent-job view a blind enqueue would risk the
		// duplicate billed calls this gate exists to prevent; skip Jev
		// Scout for this cycle and try again on the next one.
		slog.Error("candidates: skip jev-scout enqueue", "error", err)
		return nil
	}
	for _, c := range candidates {
		if held[c.InstrumentID] {
			continue
		}
		if err := r.enqueueJevScout(ctx, c.InstrumentID, c.Symbol, now); err != nil {
			// A single candidate's enqueue failure (DB write error) must
			// not drop the remaining candidates from this cycle's Jev
			// Scout pass - the same best-effort precedent
			// marketdatajob's own RAG-indexing comment follows for a
			// non-critical per-item side effect alongside a
			// already-committed primary result (here: the Screener.Set
			// above, which every candidate already reached regardless).
			slog.Error("candidates: enqueue jev-scout job failed", "instrument_id", c.InstrumentID, "symbol", c.Symbol, "error", err)
		}
	}

	return nil
}

// enqueueJevScout enqueues one jev-scout queue job (jev.ScoutJobPayload)
// for instrumentID/symbol, due immediately at now.
func (r *Refresher) enqueueJevScout(ctx context.Context, instrumentID int64, symbol string, now time.Time) error {
	payload, err := json.Marshal(jev.ScoutJobPayload{InstrumentID: instrumentID, Symbol: symbol})
	if err != nil {
		return fmt.Errorf("candidates: encode jev-scout job payload for %q: %w", symbol, err)
	}
	if _, err := r.Jobs.Enqueue(ctx, jobqueue.JobQueueJevScout, string(payload), now); err != nil {
		return fmt.Errorf("candidates: enqueue jev-scout job for %q: %w", symbol, err)
	}
	return nil
}

// Run runs Refresh on config/strategy.yaml's
// scan.candidate_refresh_interval_seconds_min/max cadence (functional.md
// §4.3's 15-30s候補銘柄更新周期) until ctx is done. Scheduler.Start's own
// doc comment explicitly defers this cycle's trigger registration to "the
// Fast Screener... scope" (i.e. this one), since Scheduler has no generic
// way to add an arbitrary extra cron/ticker beyond the ones its own
// Start hardcodes (full-scan/self-improve/log-rotation).
func (r *Refresher) Run(ctx context.Context) {
	interval := candidateRefreshInterval{
		min: time.Duration(r.Strategy.Scan.CandidateRefreshIntervalSecondsMin) * time.Second,
		max: time.Duration(r.Strategy.Scan.CandidateRefreshIntervalSecondsMax) * time.Second,
	}

	safego.Loop(ctx, "candidate refresh", interval.next, r.Refresh)
}

// candidateRefreshInterval mirrors internal/web/handler/scanner.
// CandidateRefreshInterval's Min/Max-random-jitter behavior without
// importing internal/web/handler/scanner from this file (that import only
// becomes necessary at the cmd/ call site that also needs
// scanner.CandidateRefreshInterval itself, to pass to
// router.WithCandidateRefreshInterval).
type candidateRefreshInterval struct {
	min, max time.Duration
}

func (r candidateRefreshInterval) next() time.Duration {
	if r.max <= r.min {
		return r.min
	}
	return r.min + time.Duration(rand.Int64N(int64(r.max-r.min))) //nolint:gosec // G404: refresh-interval jitter, not security-sensitive
}
