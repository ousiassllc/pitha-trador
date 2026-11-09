// Package runner is the orchestration of the metaablation CLI (issue #1046):
// `init` pins the latest rejected model and pre-registers every rule value in
// a frozen manifest BEFORE any evaluation-window data is read, and `run`
// executes a frozen manifest on a read-only database copy - the label layer,
// the replay layer, the statistics - and writes the aggregate report. Neither
// writes to the database, trains, calibrates, trades or calls an LLM.
package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// RuleOverrides replace the shipped defaults of the pre-registered rule values
// at `init` (nil keeps the default). They are recorded in the manifest and
// frozen with it; nothing can change them for a run except a new run.
type RuleOverrides struct {
	MinEVBps, InitialCapitalJPY, MaxSpreadBps, MinTurnover5mJPY *float64
	// MinContinuation, MaxToxicFlow and MaxLiquidityStressed apply to both
	// directions.
	MinContinuation, MaxToxicFlow, MaxLiquidityStressed *float64
}

func pick(v *float64, def float64) float64 {
	if v != nil {
		return *v
	}
	return def
}

// FillModelName names the replay's fill assumption in the manifest.
const FillModelName = "fillmodel (spread from the bar's bid/ask, tick grid, session phase; slippage and fee as registered)"

// DefaultRules is the pre-registered rule set: the shipped strategy/risk
// defaults (config/strategy.yaml, config/risk.yaml Paper section), the
// barrier and cost defaults and the meta setting's minimum EV, with o laid
// over. The confidence threshold is deliberately absent (no cell uses it).
func DefaultRules(o RuleOverrides) (manifest.Rules, error) {
	strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		return manifest.Rules{}, fmt.Errorf("runner: shipped strategy defaults: %w", err)
	}
	risk, err := config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	if err != nil {
		return manifest.Rules{}, fmt.Errorf("runner: shipped risk defaults: %w", err)
	}
	guards := func(t config.PolicyDirectionThresholds) manifest.FlowGuards {
		return manifest.FlowGuards{
			MinContinuationProbability: pick(o.MinContinuation, t.MinContinuationProbability),
			MaxToxicFlow:               pick(o.MaxToxicFlow, t.MaxToxicFlow),
			MaxLiquidityStressed:       pick(o.MaxLiquidityStressed, t.MaxLiquidityStressed),
		}
	}
	capital := pick(o.InitialCapitalJPY, risk.Paper.InitialCapital)
	barriers := barriersettings.Default()
	costs := costsettings.Default()
	return manifest.Rules{
		QualityCut: metaablation.QualityCut,
		Eligibility: manifest.EligibilityRules{
			MaxSpreadBps:     pick(o.MaxSpreadBps, strategy.FastScreener.MaxSpreadBps),
			MinTurnover5mJPY: pick(o.MinTurnover5mJPY, strategy.FastScreener.MinTurnover5mJPY),
		},
		Long: guards(strategy.Policy.Long), Short: guards(strategy.Policy.Short),
		MinEVBps: pick(o.MinEVBps, metalabelsettings.Default().MinEVBps),
		Label: manifest.LabelCost{
			TPBps: barriers.TakeProfitBps, SLBps: barriers.StopLossBps,
			HorizonMinutes: int(barriers.Timeout / time.Minute), CostBps: barriers.CostBps,
		},
		Replay: manifest.ReplayRules{
			InitialCapitalJPY:       capital,
			MaxPositionPerSymbolJPY: capital * risk.Paper.MaxPositionPerSymbolPct / 100,
			MaxTotalExposureJPY:     capital * risk.Paper.MaxTotalExposurePct / 100,
			StopLossPct:             execution.DefaultConfig().StopLossPct,
			FillModel:               FillModelName,
			// The spread is modelled from each bar's quote, not a parameter.
			SpreadBps: 0, SlippageBps: costs.Long.ContinuousBps, FeeBps: costs.FeeBps,
		},
	}, nil
}

// replayConfig is everything the replay layer takes from the pre-registered
// rules and the shipped Paper defaults. The manifest records the registered
// part (capital, exposure limits, stop loss, slippage, fee, guards) and a
// fingerprint of the whole, so a run refuses a code whose defaults moved.
type replayConfig struct {
	Risk       simrisk.Config
	Exit       backtest.ExitRule
	Cost       fillmodel.Costs
	Thresholds policy.Thresholds
}

// newReplayConfig builds the replay configuration of r: the Paper risk
// limits of config/risk.yaml with the registered capital and exposure limits,
// the exit rule of execution.ConfigFromRiskLimits with the registered stop
// loss, the default costs with the registered slippage and fee, and the
// registered eligibility and flow-guard values. The buckets' budgets are the
// default split (the stock bucket the whole capital).
func newReplayConfig(r manifest.Rules) (replayConfig, error) {
	risk, err := config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	if err != nil {
		return replayConfig{}, fmt.Errorf("runner: shipped risk defaults: %w", err)
	}
	rp := r.Replay
	if rp.SpreadBps != 0 {
		return replayConfig{}, fmt.Errorf("runner: replay.spread_bps %v: the fill model takes the spread from each bar's quote and has no spread parameter", rp.SpreadBps)
	}
	limits := risk.Paper
	limits.InitialCapital = rp.InitialCapitalJPY
	limits.MaxPositionPerSymbolPct = rp.MaxPositionPerSymbolJPY / rp.InitialCapitalJPY * 100
	limits.MaxTotalExposurePct = rp.MaxTotalExposureJPY / rp.InitialCapitalJPY * 100
	exec := execution.ConfigFromRiskLimits(limits)
	costSettings := costsettings.Default()
	costSettings.FeeBps = rp.FeeBps
	costSettings.Long.ContinuousBps, costSettings.Short.ContinuousBps = rp.SlippageBps, rp.SlippageBps
	side := func(g manifest.FlowGuards) config.PolicyDirectionThresholds {
		return config.PolicyDirectionThresholds{
			MinContinuationProbability: g.MinContinuationProbability, MaxToxicFlow: g.MaxToxicFlow, MaxLiquidityStressed: g.MaxLiquidityStressed,
		}
	}
	return replayConfig{
		Risk: simrisk.Config{Limits: limits, StopLossPct: rp.StopLossPct},
		Exit: backtest.ExitRule{
			StopLossPct: rp.StopLossPct, TakeProfitPct: exec.TakeProfitPct, TrailingStopPct: exec.TrailingStopPct,
			MaxHolding:                 time.Duration(exec.MaxHoldingMinutes) * time.Minute,
			MinContinuationProbability: exec.MinContinuationProbability,
			ForceFlatBeforeClose:       time.Duration(exec.ForceFlatBeforeMarketCloseMinutes) * time.Minute,
			CooldownAfterLoss:          time.Duration(exec.CooldownAfterLossMinutes) * time.Minute,
		},
		Cost: fillmodel.FromSettings(costSettings),
		Thresholds: policy.Thresholds{
			Policy:       config.PolicyConfig{Long: side(r.Long), Short: side(r.Short)},
			MaxSpreadBps: r.Eligibility.MaxSpreadBps, MinTurnover5mJPY: r.Eligibility.MinTurnover5mJPY,
		},
	}, nil
}

// fingerprintNote is the manifest note that pins the whole replay configuration.
const fingerprintNote = "replay_config_sha256="

// fingerprint is the SHA-256 of the replay configuration (risk limits, exit
// rule, costs, thresholds), recorded in the manifest at `init`.
func (c replayConfig) fingerprint() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("runner: encode replay configuration: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// replayNotes are the provenance notes that pin the replay configuration.
func replayNotes(r manifest.Rules) ([]string, error) {
	cfg, err := newReplayConfig(r)
	if err != nil {
		return nil, err
	}
	fp, err := cfg.fingerprint()
	if err != nil {
		return nil, err
	}
	l := cfg.Risk.Limits
	return []string{
		fingerprintNote + fp,
		fmt.Sprintf("replay risk limits are config/risk.yaml's Paper section (max_daily_loss_pct %.2f, max_trade_loss_pct %.2f, max_open_positions %d, max_same_direction_positions %d, max_consecutive_losses %d, cooldown %d min) with the registered capital and exposure limits; exits are execution.ConfigFromRiskLimits with the registered stop loss; bucket budgets are the default split",
			l.MaxDailyLossPct, l.MaxTradeLossPct, l.MaxOpenPositions, l.MaxSameDirectionPositions, l.MaxConsecutiveLosses, l.CooldownAfterLossMinutes),
	}, nil
}
