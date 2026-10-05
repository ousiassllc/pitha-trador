package backtest

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// DecisionSource supplies the historical Jev Trader decision Policy
// Engine needs to evaluate the bar at ts for instrumentID (functional.md
// §4.5/§4.6). Backtest replay is decoupled from exactly how decisions
// were captured: judgement.DecisionRepository's current schema
// (db/migrations/000004_create_jev_decisions_table.up.sql) does not
// persist every domain.JevDecision field a Policy Engine decision needs
// (EntryQuality/ContinuationProbability/ToxicFlow/LiquidityStressed -
// internal/repository/judgement/decision_repo.go only round-trips
// Direction/Confidence on re-read), so a caller supplies a source built
// from decisions it already holds in full (e.g. a Self-Improvement
// Governor replaying its own recent jev.Trader.Evaluate results for a
// shadow backtest, or a test fixture) rather than this package reading a
// lossy jev_decisions row itself.
type DecisionSource interface {
	// Decision returns the most recent domain.JevDecision timestamped at
	// or before ts for instrumentID, or ok=false if none exists yet -
	// replay then leaves policy.Input.Decision nil, matching how the
	// live pipeline treats "no Jev Trader decision available"
	// (policy.ReasonMissingData).
	Decision(ctx context.Context, instrumentID int64, ts time.Time) (decision domain.JevDecision, ok bool, err error)
}

// SliceDecisionSource is a DecisionSource backed by an in-memory slice of
// domain.JevDecision - the shape a caller already holding decisions (a
// shadow backtest's recent Jev Trader calls, or a test fixture) most
// naturally has.
type SliceDecisionSource struct {
	decisions []domain.JevDecision // sorted ascending by Timestamp
}

// NewSliceDecisionSource returns a SliceDecisionSource over decisions,
// sorting a copy by Timestamp.
func NewSliceDecisionSource(decisions []domain.JevDecision) *SliceDecisionSource {
	sorted := make([]domain.JevDecision, len(decisions))
	copy(sorted, decisions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })
	return &SliceDecisionSource{decisions: sorted}
}

// Decision implements DecisionSource, scanning for the latest decision at
// or before ts for instrumentID.
func (s *SliceDecisionSource) Decision(_ context.Context, instrumentID int64, ts time.Time) (domain.JevDecision, bool, error) {
	var best domain.JevDecision
	found := false
	for _, d := range s.decisions {
		if d.InstrumentID != instrumentID {
			continue
		}
		if d.Timestamp.After(ts) {
			break
		}
		best, found = d, true
	}
	return best, found, nil
}

// ExitRule is the simplified subset of FR-EXIT-1 this backtest replay
// evaluates bar-by-bar to close a position once Policy Engine opens one:
// fixed Stop Loss, fixed Take Profit, and max holding time
// (config/strategy.yaml has no exit.* section yet - FR-EXIT-2's initial
// values are stop_loss_pct=0.6 / take_profit_pct=1.2 /
// max_holding_minutes=20 - so a caller passes them explicitly). Trailing
// Stop / Jev方向反転 / continuation_probability低下 / VWAP逆クロス / 引け
// 前強制決済 (FR-EXIT-1's remaining conditions) belong to Risk Engine &
// Paper Trading Execution (issue #8, a later sub-scope) once real
// position management exists; this subset is enough to close every
// backtest position deterministically and produce FR-BT-1's metrics
// end-to-end. A zero-valued field disables that condition.
type ExitRule struct {
	StopLossPct   float64
	TakeProfitPct float64
	MaxHolding    time.Duration
}

// RunConfig is everything one backtest replay needs for a single
// instrument (FR-BT-1〜3).
type RunConfig struct {
	InstrumentID int64
	Symbol       string

	// Snapshots is the exact, caller-resolved ascending-by-Timestamp
	// Snapshot series to replay - the "look-ahead防止のためのデータ供給
	// 境界" this package's own doc.go describes. A caller typically
	// builds this via market.SnapshotRepository.ListByInstrumentRange
	// covering at least the full range any Period passed to
	// Run/ShadowBacktest spans, plus whatever leading history Feature
	// Engine needs to warm up Return5m/15m/RealizedVol5m for the first
	// bars in that range.
	Snapshots []domain.Snapshot
	// WarmupBars is how many leading Snapshots bars are history only:
	// their own Feature values were computed from bars before the slice,
	// so VerifyNoLookahead skips them (they must also precede every
	// evaluated Period, since replay would otherwise trade on them).
	WarmupBars int

	Decisions DecisionSource

	// Thresholds is the Policy Engine LONG/SHORT threshold set to
	// evaluate against - the value a Self-Improvement Governor shadow
	// backtest overrides with a proposed change (overview.md §8
	// "GOV->>BT: 直近20営業日相当のシャドーバックテスト実行（提案後しき
	// い値）") without touching the live runtime_settings.
	Thresholds policy.Thresholds
	// Risk defaults to policy.AlwaysPassRiskChecker when nil (via
	// policy.NewEngine), as doc.go explains: the live Risk Engine's limits
	// depend on live account/Kill Switch state a historical replay does
	// not have (functional.md §4.11 FR-BT-4).
	Risk policy.RiskChecker

	Exit ExitRule
	// Cost is the fill assumption (fillmodel.Default in production, the
	// same one Paper Trading uses): FR-BT-1's スリッページ込み/手数料込みPnL
	// figures come from the fills it produces.
	Cost fillmodel.Model
	// Sessions says which bars are 昼休み/寄り/ザラ場/引け (see Sessions);
	// nil means every bar is ザラ場.
	Sessions Sessions
}

// SplitResult is one Walk Forward fold's Split boundaries plus the
// Metrics computed independently for its Training, Validation and
// Forward periods.
type SplitResult struct {
	Split      Split
	Training   Metrics
	Validation Metrics
	Forward    Metrics
}

// Result is a full Walk Forward Run's output (FR-BT-2). Combined is the
// aggregate across every fold's Forward-period trades only -
// Training/Validation trades never contribute to it - the walk-forward,
// out-of-sample headline result FR-BT-2 requires ("全期間一括最適化しな
// い").
type Result struct {
	Splits   []SplitResult
	Combined Metrics
}

// Run executes a full Walk Forward backtest (FR-BT-2) over a single
// instrument's cfg using wf's fold boundaries - RunPortfolio with one
// RunConfig.
func Run(ctx context.Context, cfg RunConfig, wf WalkForwardConfig) (Result, error) {
	return RunPortfolio(ctx, []RunConfig{cfg}, wf)
}

// replay walks cfg.Snapshots forward through window, evaluating Policy
// Engine at every bar whose Timestamp falls in [window.Start, window.End)
// (calibrated fixes policy.Input.Calibrated for all of them), opening a
// Trade whenever it produces a LONG/SHORT signal and closing it per
// cfg.Exit/cfg.Cost before resuming - one open position at a time per
// instrument, matching functional.md §4.9's per-symbol "position" state.
func replay(ctx context.Context, cfg RunConfig, window Period, calibrated bool) ([]Trade, error) {
	bars := cfg.Snapshots
	engine := policy.NewEngine(cfg.Thresholds, cfg.Risk, nil)

	start := sort.Search(len(bars), func(k int) bool { return !bars[k].Timestamp.Before(window.Start) })

	var trades []Trade
	var consumedUpTo time.Time
	for i := start; i < len(bars); {
		bar := bars[i]
		if !bar.Timestamp.Before(window.End) {
			break
		}

		decision, ok, err := cfg.Decisions.Decision(ctx, cfg.InstrumentID, bar.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("backtest: decision lookup for %q at %s: %w", cfg.Symbol, bar.Timestamp, err)
		}
		// Each Jev Trader decision is evaluated at most once (FR-BT-4):
		// live, one decision yields at most one Policy evaluation/signal,
		// so a decision already evaluated (or skipped while a position was
		// open) is treated as absent rather than re-entered after an exit.
		if ok && !decision.Timestamp.After(consumedUpTo) {
			ok = false
		}

		in := policy.Input{
			InstrumentID:        cfg.InstrumentID,
			Symbol:              cfg.Symbol,
			Timestamp:           bar.Timestamp,
			EntryPriceReference: &bar.Price,
			SpreadBps:           bar.SpreadBps,
			Turnover5mJPY:       bar.Feature.Turnover5m,
			Calibrated:          calibrated,
		}
		if ok {
			in.Decision = &decision
			consumedUpTo = decision.Timestamp
		}

		sig := engine.Decide(ctx, in)
		if sig.Direction == domain.JevDirectionNone {
			i++
			continue
		}

		x := executor{cost: cfg.Cost, sessions: cfg.Sessions}
		entryFill, ok := x.fill(bar, entrySide(sig.Direction))
		if !ok {
			// 昼休み・立会時間外: the entry cannot fill (live Enter fails
			// with ErrOutsideTradingSession), and the decision is spent.
			i++
			continue
		}

		trade, exitIdx := closeTrade(bars, i, sig.Direction, entryFill, cfg.Exit, x, cfg.InstrumentID, cfg.Symbol)
		if exitIdx == i {
			// No bar to hold through (last bar, or the next bar is already
			// past MaxHolding - session/overnight gap): the entry could not
			// have been filled and held, so it is not a trade (zero-return
			// entry==exit rows would dilute TradeCount/Expectancy/WinRate).
			i++
			continue
		}
		trades = append(trades, trade)
		consumedUpTo = bars[exitIdx].Timestamp
		i = exitIdx + 1
	}
	return trades, nil
}
