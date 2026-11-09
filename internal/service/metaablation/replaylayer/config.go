package replaylayer

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

// QualityOffFloor is the entry_quality floor of Q off: the lowest quality, which
// every decision (including an unrecognized value, ranked lowest) clears - the
// entry_quality cut is not applied. The other guards keep their values.
const QualityOffFloor = domain.JevEntryQualityPoor

// Entry rules of a cell. M on is the live meta-labeling rule (backtest.EntryMeta).
// M off is the same guards and Risk Engine gate with no model verdict: the
// engine's non-meta path (backtest.EntryLegacyConfidence) with
// policy.*.min_probability set to 0, which makes its confidence condition vacuous
// - so the old confidence threshold is used by no cell. Guard order, reasons and
// the reversion rule are those of the live tree.
const (
	EntryMOn  = "meta"
	EntryMOff = "guards_only_min_probability_0"
)

// DirectionGuards are the shared guard values of one direction. The old
// confidence threshold (min_probability) is deliberately absent: no cell uses it.
type DirectionGuards struct {
	MinContinuationProbability float64
	MaxToxicFlow               float64
	MaxLiquidityStressed       float64
}

// ConfigRecord is the exact configuration of the replay, identical for the four
// cells, for the manifest. It holds no model parameters.
type ConfigRecord struct {
	InitialCapitalJPY float64
	Risk              simrisk.Config
	Cost              fillmodel.Costs
	Exit              backtest.ExitRule
	MaxSpreadBps      float64
	MinTurnover5mJPY  float64
	Long, Short       DirectionGuards
	// QualityCut is the Q-on floor (fixed); QualityOffFloor the Q-off floor.
	QualityCut      string
	QualityOffFloor string
	// MinEVBps is the pre-registered minimum EV of the M-on cells.
	MinEVBps      float64
	KellyFraction float64
	ModelID       int64
	ModelVersion  string
	// MinProbability is the old confidence threshold's value in every cell: 0,
	// i.e. unused (the M-on rule ignores it, the M-off rule has it vacuous).
	MinProbability float64
	// SegmentPolicy states how gaps split the window.
	SegmentPolicy string
}

// SegmentPolicy is ConfigRecord.SegmentPolicy.
const SegmentPolicy = "every unavailable period splits the window into common continuous segments; each segment " +
	"starts from the initial capital with no open position, streak or cooldown; segments are never stitched"

func (in Input) configRecord() ConfigRecord {
	guards := func(t config.PolicyDirectionThresholds) DirectionGuards {
		return DirectionGuards{t.MinContinuationProbability, t.MaxToxicFlow, t.MaxLiquidityStressed}
	}
	return ConfigRecord{
		InitialCapitalJPY: in.capital(), Risk: in.Risk, Cost: in.Cost, Exit: in.Exit,
		MaxSpreadBps: in.Thresholds.MaxSpreadBps, MinTurnover5mJPY: in.Thresholds.MinTurnover5mJPY,
		Long: guards(in.Thresholds.Policy.Long), Short: guards(in.Thresholds.Policy.Short),
		QualityCut: metaablation.QualityCut, QualityOffFloor: QualityOffFloor,
		MinEVBps: in.Meta.MinEVBps, KellyFraction: in.Meta.KellyFraction,
		MinProbability: 0, ModelID: in.Model.ID, ModelVersion: in.Model.Version,
		SegmentPolicy: SegmentPolicy,
	}
}

// cellConfig is what a cell changes.
func cellConfig(c metaablation.Cell) CellConfig {
	cc := CellConfig{Entry: EntryMOff, MinEntryQuality: QualityOffFloor, ModelApplied: c.MOn}
	if c.MOn {
		cc.Entry = EntryMOn
	}
	if c.QOn {
		cc.MinEntryQuality = metaablation.QualityCut
	}
	return cc
}

// fixedModel is the one model of the comparison, in force at every decision time:
// the evaluation rows are independent (later than the model's training), so no
// "model in force then" timeline applies. It is the stored model as given.
type fixedModel struct{ model *metalabel.Model }

func (f fixedModel) ModelAt(time.Time) (*metalabel.Model, error) { return f.model, nil }

// runConfigs builds the per-instrument backtest configs of one cell on one
// segment. It is the only place the cells differ, and only in Entry, Models and
// the two entry_quality floors (and Entry's M-off form, see EntryMOff).
func (in Input) runConfigs(c metaablation.Cell, seg SegmentSpec, decisions backtest.DecisionSource, observe func(backtest.EntryEvaluation)) []backtest.RunConfig {
	cc := cellConfig(c)
	th := in.Thresholds
	th.Policy.Long.MinEntryQuality, th.Policy.Short.MinEntryQuality = cc.MinEntryQuality, cc.MinEntryQuality
	th.Policy.Long.MinProbability, th.Policy.Short.MinProbability = 0, 0
	risk := in.Risk
	cfgs := make([]backtest.RunConfig, 0, len(in.Instruments))
	for _, inst := range in.Instruments {
		bars, warmup := segmentBars(inst.Snapshots, seg.From, seg.To)
		cfg := backtest.RunConfig{
			InstrumentID: inst.InstrumentID, Symbol: inst.Symbol, AssetClass: inst.AssetClass,
			Snapshots: bars, WarmupBars: warmup, Decisions: decisions,
			Entry: backtest.EntryLegacyConfidence, Meta: in.Meta, Thresholds: th, Risk: &risk,
			Exit: in.Exit, Cost: in.Cost, Sessions: in.Sessions, OnEntryEvaluation: observe,
		}
		if c.MOn {
			cfg.Entry, cfg.Models = backtest.EntryMeta, fixedModel{in.Model}
		}
		cfgs = append(cfgs, cfg)
	}
	return cfgs
}

// commonPart is cfgs without what a cell may change (and without the observer),
// the snapshots reduced to their identity: what must be equal in all cells.
func commonPart(cfgs []backtest.RunConfig) []backtest.RunConfig {
	out := make([]backtest.RunConfig, len(cfgs))
	for i, cfg := range cfgs {
		cfg.Entry, cfg.Models, cfg.OnEntryEvaluation = "", nil, nil
		cfg.Thresholds.Policy.Long.MinEntryQuality, cfg.Thresholds.Policy.Short.MinEntryQuality = "", ""
		cfg.Snapshots = nil
		out[i] = cfg
	}
	return out
}

// sameInputs reports whether two configs replay the identical snapshot slices.
func sameInputs(a, b []backtest.RunConfig) bool {
	for i := range a {
		x, y := a[i].Snapshots, b[i].Snapshots
		if len(x) != len(y) || (len(x) > 0 && &x[0] != &y[0]) {
			return false
		}
	}
	return len(a) == len(b)
}

// assertCommon fails when the cells' configs differ in anything but the axes:
// eligibility, guards, Risk, capital, exits, fills, costs and inputs.
func assertCommon(cells [][]backtest.RunConfig) error {
	for i := 1; i < len(cells); i++ {
		if !sameInputs(cells[0], cells[i]) || !reflect.DeepEqual(commonPart(cells[0]), commonPart(cells[i])) {
			return fmt.Errorf("replaylayer: cell %d's configuration differs from cell 0's beyond the Q and M axes", i)
		}
		for _, cfg := range cells[i] {
			p := cfg.Thresholds.Policy
			if p.Long.MinProbability != 0 || p.Short.MinProbability != 0 {
				return errors.New("replaylayer: the old confidence threshold must be 0 (unused) in every cell")
			}
		}
	}
	return nil
}
