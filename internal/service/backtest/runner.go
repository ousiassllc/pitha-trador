package backtest

import (
	"context"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
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
	byInstrument map[int64][]domain.JevDecision // each sorted ascending by Timestamp
}

// NewSliceDecisionSource returns a SliceDecisionSource over decisions,
// grouping a copy by instrument and sorting each group by Timestamp.
func NewSliceDecisionSource(decisions []domain.JevDecision) *SliceDecisionSource {
	byInstrument := make(map[int64][]domain.JevDecision)
	for _, d := range decisions {
		byInstrument[d.InstrumentID] = append(byInstrument[d.InstrumentID], d)
	}
	for _, list := range byInstrument {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Timestamp.Before(list[j].Timestamp) })
	}
	return &SliceDecisionSource{byInstrument: byInstrument}
}

// Decision implements DecisionSource: the latest decision at or before ts for
// instrumentID (the last one given on a timestamp tie).
func (s *SliceDecisionSource) Decision(_ context.Context, instrumentID int64, ts time.Time) (domain.JevDecision, bool, error) {
	list := s.byInstrument[instrumentID]
	n := sort.Search(len(list), func(i int) bool { return list[i].Timestamp.After(ts) })
	if n == 0 {
		return domain.JevDecision{}, false, nil
	}
	return list[n-1], true, nil
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

	// Entry selects the entry rule. The zero value is EntryMeta, the live
	// rule: the meta-labeling model's verdict (issue #775).
	Entry EntryMode
	// Models supplies, for each decision time, the meta-labeling model that
	// was already trained and active then (EntryMeta). nil means no model ever
	// existed: every entry is skipped, as live does without an active model.
	Models ModelSource
	// Meta is the meta-labeling take/skip policy (minimum EV); the zero value
	// means metalabel.DefaultDecideConfig.
	Meta metalabel.DecideConfig

	// Thresholds is the Policy Engine LONG/SHORT threshold set to
	// evaluate against - the value a Self-Improvement Governor shadow
	// backtest overrides with a proposed change (overview.md §8
	// "GOV->>BT: 直近20営業日相当のシャドーバックテスト実行（提案後しき
	// い値）") without touching the live runtime_settings.
	Thresholds policy.Thresholds
	// Risk, when set, runs the replay through a simulated Risk Engine
	// (simrisk): the FR-RISK-1/2 limits against a portfolio shared by every
	// instrument of the replay, and position sizing. nil replays without any
	// Risk Engine (an explicit comparison mode). Every RunConfig of one
	// portfolio replay must carry the same Risk.
	Risk *simrisk.Config

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
	// ByRegime is Combined split by the market regime of the decision that
	// opened each trade (issue #755).
	ByRegime []RegimeMetrics
}

// Run executes a full Walk Forward backtest (FR-BT-2) over a single
// instrument's cfg using wf's fold boundaries - RunPortfolio with one
// RunConfig.
func Run(ctx context.Context, cfg RunConfig, wf WalkForwardConfig) (Result, error) {
	return RunPortfolio(ctx, []RunConfig{cfg}, wf)
}
