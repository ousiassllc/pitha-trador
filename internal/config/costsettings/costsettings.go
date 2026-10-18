// Package costsettings holds the Settings-screen configuration of the trading
// cost assumption (issue #779): the commission (bps of notional and a fixed
// JPY amount per order), the slippage by trade direction (LONG/SHORT) and
// session (ザラ場/寄り/引け) and the SHORT 貸株料. Paper fills
// (internal/service/execution), the backtest (internal/bootstrap/backtestsource)
// and the triple-barrier label cost (internal/service/calibration, hence the
// meta-labeling EV) all read this one set. It lives in runtime_settings
// (docs/architecture/er.md §runtime_settings) under Keys; there is no yaml or
// environment source.
//
// The shipped defaults equal the cost behaviour before the settings existed:
// no commission, 2 bps slippage in the ザラ場 and 5 bps in the 寄り/引け
// auctions, no 貸株料. Nothing changes until the operator edits a value.
package costsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// runtime_settings keys. Every value is a JSON number: bps, or JPY for the
// *_jpy keys.
const (
	KeyFeeBps                  = "cost.fee_bps"
	KeyFeeFixedJPY             = "cost.fee_fixed_jpy"
	KeySlippageLongContinuous  = "cost.slippage_long_continuous_bps"
	KeySlippageLongOpening     = "cost.slippage_long_opening_bps"
	KeySlippageLongClosing     = "cost.slippage_long_closing_bps"
	KeySlippageShortContinuous = "cost.slippage_short_continuous_bps"
	KeySlippageShortOpening    = "cost.slippage_short_opening_bps"
	KeySlippageShortClosing    = "cost.slippage_short_closing_bps"
	KeyShortBorrowBps          = "cost.short_borrow_bps"
	KeyReferenceNotionalJPY    = "cost.reference_notional_jpy"
)

// Accepted ranges.
const (
	// MaxBps is the largest bps value accepted for any cost.
	MaxBps = 1000.0
	// MaxFixedJPY is the largest fixed commission per order accepted.
	MaxFixedJPY = 100_000.0
	// MaxReferenceNotionalJPY bounds the reference order size.
	MaxReferenceNotionalJPY = 1_000_000_000.0
)

// Shipped defaults: the pre-#779 fill model. DefaultReferenceNotionalJPY only
// converts the fixed commission into bps for the callers that have no order
// size (backtest percentage returns, triple-barrier labels, the sensitivity
// table); with the default fixed commission of 0 it has no effect.
const (
	DefaultContinuousSlippageBps = 2.0
	DefaultAuctionSlippageBps    = 5.0
	DefaultReferenceNotionalJPY  = 1_000_000.0
)

// Slippage is the slippage, in bps beyond the touch (ザラ場) or the indicative
// price (寄り/引け), of one trade direction by session.
type Slippage struct {
	ContinuousBps float64
	OpeningBps    float64
	ClosingBps    float64
}

// Settings is the effective trading cost assumption.
type Settings struct {
	// FeeBps is the commission charged on each fill's notional (bps).
	FeeBps float64
	// FeeFixedJPY is a commission charged per order (fill) in JPY, on top of
	// FeeBps.
	FeeFixedJPY float64
	// Long and Short are the slippage of a LONG and a SHORT position's fills
	// (entry and exit alike).
	Long, Short Slippage
	// ShortBorrowBps is the SHORT 貸株料 (stock-loan fee) charged once per
	// SHORT position as bps of its entry notional. Positions never carry over
	// the day, so 逆日歩 and overnight interest do not arise (see
	// docs/requirements/functional/components-platform.md FR-COST-1).
	ShortBorrowBps float64
	// ReferenceNotionalJPY is the order size FeeFixedJPY is converted to bps
	// at where no size exists.
	ReferenceNotionalJPY float64
}

// Default returns the shipped assumption: fee 0, slippage 2 bps (ザラ場) / 5
// bps (寄り・引け) for both directions, no 貸株料.
func Default() Settings {
	s := Slippage{ContinuousBps: DefaultContinuousSlippageBps, OpeningBps: DefaultAuctionSlippageBps, ClosingBps: DefaultAuctionSlippageBps}
	return Settings{Long: s, Short: s, ReferenceNotionalJPY: DefaultReferenceNotionalJPY}
}

// Keys returns every runtime_settings key Apply accepts.
func Keys() []string {
	return []string{
		KeyFeeBps, KeyFeeFixedJPY,
		KeySlippageLongContinuous, KeySlippageLongOpening, KeySlippageLongClosing,
		KeySlippageShortContinuous, KeySlippageShortOpening, KeySlippageShortClosing,
		KeyShortBorrowBps, KeyReferenceNotionalJPY,
	}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	for _, k := range Keys() {
		if k == key {
			return true
		}
	}
	return false
}

// field returns the Settings field key addresses, or nil for another key.
func (s *Settings) field(key string) *float64 {
	switch key {
	case KeyFeeBps:
		return &s.FeeBps
	case KeyFeeFixedJPY:
		return &s.FeeFixedJPY
	case KeySlippageLongContinuous:
		return &s.Long.ContinuousBps
	case KeySlippageLongOpening:
		return &s.Long.OpeningBps
	case KeySlippageLongClosing:
		return &s.Long.ClosingBps
	case KeySlippageShortContinuous:
		return &s.Short.ContinuousBps
	case KeySlippageShortOpening:
		return &s.Short.OpeningBps
	case KeySlippageShortClosing:
		return &s.Short.ClosingBps
	case KeyShortBorrowBps:
		return &s.ShortBorrowBps
	case KeyReferenceNotionalJPY:
		return &s.ReferenceNotionalJPY
	}
	return nil
}

// limit is the largest value key accepts.
func limit(key string) float64 {
	switch key {
	case KeyFeeFixedJPY:
		return MaxFixedJPY
	case KeyReferenceNotionalJPY:
		return MaxReferenceNotionalJPY
	}
	return MaxBps
}

// Text returns s's value for key as the Settings screen shows it, or ok=false
// when key is not one of Keys.
func (s Settings) Text(key string) (text string, ok bool) {
	f := s.field(key)
	if f == nil {
		return "", false
	}
	return fmt.Sprintf("%g", *f), true
}

// Apply writes jsonValue - a runtime_settings.value, a JSON number - into the
// field of s named by key, checking the value's range.
func Apply(s *Settings, key, jsonValue string) error {
	f := s.field(key)
	if f == nil {
		return fmt.Errorf("costsettings: %q is not a cost setting key", key)
	}
	var n float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil {
		return fmt.Errorf("costsettings: decode runtime setting %s=%q as a number: %w", key, jsonValue, err)
	}
	if err := checkRange(key, n); err != nil {
		return fmt.Errorf("costsettings: runtime setting %s=%q: %w", key, jsonValue, err)
	}
	*f = n
	return nil
}

func checkRange(key string, n float64) error {
	max := limit(key)
	lowest := 0.0
	if key == KeyReferenceNotionalJPY {
		lowest = 1
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < lowest || n > max {
		return fmt.Errorf("%g 以上 %g 以下で指定してください", lowest, max)
	}
	return nil
}

// Validate checks every field's range. Violations are named by
// runtime_settings key and joined.
func (s Settings) Validate() error {
	var errs []error
	for _, key := range Keys() {
		f := (&s).field(key)
		if err := checkRange(key, *f); err != nil {
			errs = append(errs, fmt.Errorf("%s は %w", key, err))
		}
	}
	return errors.Join(errs...)
}

// Store is the runtime_settings read access Load needs;
// *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Load returns the effective cost assumption: each stored row over its
// default. A stored set that no longer parses or validates (the Settings
// screen stores only valid ones) is logged and replaced by the defaults, so a
// damaged row cannot stop a fill or a label; only a store read failure is an
// error.
func Load(ctx context.Context, store Store) (Settings, error) {
	s := Default()
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Default(), fmt.Errorf("costsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Apply(&s, key, raw); err != nil {
			slog.Warn("costsettings: ignoring invalid stored cost settings; using defaults", "error", err)
			return Default(), nil
		}
	}
	return s, nil
}

// SlippageOf is the slippage table of direction (domain.JevDirectionLong or
// Short; any other value is treated as LONG).
func (s Settings) SlippageOf(direction string) Slippage {
	if direction == domain.JevDirectionShort {
		return s.Short
	}
	return s.Long
}

// FixedFeeBps is the fixed commission of one order expressed in bps of the
// reference notional.
func (s Settings) FixedFeeBps() float64 {
	if s.ReferenceNotionalJPY <= 0 {
		return 0
	}
	return s.FeeFixedJPY / s.ReferenceNotionalJPY * 10_000
}

// RoundTripBps is the cost, in bps of the entry price, the fill model charges
// one ザラ場 round trip of direction beyond the spread: the commission on both
// fills, the slippage on both fills and, for SHORT, the 貸株料. The spread is
// not part of it: it comes from the board of each bar.
func (s Settings) RoundTripBps(direction string) float64 {
	cost := 2*(s.FeeBps+s.FixedFeeBps()) + 2*s.SlippageOf(direction).ContinuousBps
	if direction == domain.JevDirectionShort {
		cost += s.ShortBorrowBps
	}
	return cost
}

// LabelCostBps is the cost the triple-barrier label of direction assumes: base
// (Settings' calibration.barrier_cost_bps: fees + spread + slippage in one
// all-in figure) moved by how far the modelled round-trip cost is from the
// shipped defaults. At the defaults it is base unchanged; raise the fee or the
// slippage and the label cost rises with it, so the label (and the
// meta-labeling EV trained on it) never ignores the cost the fills charge. It
// is never negative.
func (s Settings) LabelCostBps(base float64, direction string) float64 {
	return math.Max(0, base+s.RoundTripBps(direction)-Default().RoundTripBps(direction))
}

// LabelBarriers returns b with its CostBps moved by the cost settings read from
// store (Settings.LabelCostBps) for direction: the barriers a triple-barrier
// label of that direction is measured with.
func LabelBarriers(ctx context.Context, store Store, b barriersettings.Barriers, direction string) (barriersettings.Barriers, error) {
	s, err := Load(ctx, store)
	if err != nil {
		return b, err
	}
	b.CostBps = s.LabelCostBps(b.CostBps, direction)
	return b, nil
}
