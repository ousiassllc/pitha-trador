package selfimprove

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
)

// shadowBacktestLookbackDays is FR-SELFIMPROVE-4's "直近20営業日相当".
const shadowBacktestLookbackDays = 20

// postApplyTrackingDays is FR-SELFIMPROVE-6's "適用後5営業日相当".
const postApplyTrackingDays = 5

// expectancyDegradationTolerance is FR-SELFIMPROVE-6's rollback
// trigger: "適用前より相対20%以上悪化した場合".
const expectancyDegradationTolerance = 0.20

// ShadowBacktestSource supplies the backtest.RunConfig(s) a shadow
// backtest replays over period (FR-SELFIMPROVE-4's "直近の
// trade_signals/jev_decisions/calibration_outcomes"): one RunConfig per
// instrument, its Thresholds already set to the *current* (pre-proposal)
// policy.* values - Governor overrides only RunConfig.Thresholds.Policy
// for the candidate pass. internal/bootstrap/backtestsource's Source is the
// production implementation (RunConfigs assembled from the recorded
// market_snapshots/jev_decisions under RuntimePolicy's thresholds).
type ShadowBacktestSource interface {
	RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error)
}

// Governor is the Self-Improvement Governor (overview.md §8): it records
// Sol's proposals, runs Opus's shadow-backtest review, applies approved
// proposals to runtime_settings, and rolls back proposals whose
// post-apply realized Expectancy degrades (FR-SELFIMPROVE-1〜7).
type Governor struct {
	proposals *judgement.ProposalRepository
	settings  *system.RuntimeSettingsRepository
	positions *trading.PositionRepository
	source    ShadowBacktestSource

	sol      *assist.Sol
	opus     *assist.Opus
	notifier Notifier
	now      func() time.Time

	// baseline is config/strategy.yaml's static policy.* defaults,
	// used for any of the 10 PolicyProposalKeys runtime_settings has no
	// override for yet.
	baseline config.PolicyConfig
}

// Option configures a Governor beyond NewGovernor's required
// dependencies.
type Option func(*Governor)

// WithNotifier overrides the default NoopNotifier.
func WithNotifier(n Notifier) Option {
	return func(g *Governor) { g.notifier = n }
}

// WithSol sets the Sol adapter (the external Sol LLM API client). Without
// it Sol is unconfigured: every daily analysis is skipped with
// assist.ErrNotConfigured.
func WithSol(sol *assist.Sol) Option {
	return func(g *Governor) { g.sol = sol }
}

// WithOpus sets the Opus adapter (the external Opus LLM API client).
// Without it Opus is unconfigured: no proposal that meets the
// deterministic thresholds can be approved.
func WithOpus(opus *assist.Opus) Option {
	return func(g *Governor) { g.opus = opus }
}

// WithNow overrides time.Now (tests).
func WithNow(now func() time.Time) Option {
	return func(g *Governor) { g.now = now }
}

// NewGovernor returns a Governor. baseline is config/strategy.yaml's
// PolicyConfig (config.LoadStrategy's result), the fallback for any
// policy.* key runtime_settings has no override for.
func NewGovernor(
	proposals *judgement.ProposalRepository,
	settings *system.RuntimeSettingsRepository,
	positions *trading.PositionRepository,
	source ShadowBacktestSource,
	baseline config.PolicyConfig,
	opts ...Option,
) *Governor {
	g := &Governor{
		proposals: proposals,
		settings:  settings,
		positions: positions,
		source:    source,
		sol:       assist.NewSol(assist.NewClient(assist.Config{Label: "sol"})),
		opus:      assist.NewOpus(assist.NewClient(assist.Config{Label: "opus"})),
		notifier:  NoopNotifier{},
		now:       func() time.Time { return time.Now().UTC() },
		baseline:  baseline,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// CurrentThresholds returns the currently-active PolicyConfig
// (RuntimePolicy.CurrentThresholds over g.baseline and g.settings).
func (g *Governor) CurrentThresholds(ctx context.Context) (config.PolicyConfig, error) {
	return NewRuntimePolicy(g.settings, g.proposals, g.baseline).CurrentThresholds(ctx)
}
