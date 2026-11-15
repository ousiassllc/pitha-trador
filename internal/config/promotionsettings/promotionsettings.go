// Package promotionsettings holds the numeric criteria of the promotion checklist
// (issue #1091: the Paper results MCP's promotion_checklist tool): how many common rows
// and days a candidate needs, how far the lower end of its cost-inclusive difference to
// the current one must lie above zero, how many days must favour it, and how small the
// multiple-comparison-adjusted p-value must be.
//
// The criteria are documented in docs/architecture/research-tools/paper-results-mcp-promotion.md. The
// documented value of each is its Floor, and a setting can only make a criterion
// STRICTER than its floor (a larger minimum, a smaller maximum): a value that is looser
// is refused when it is saved (Validate) and ignored, with the floor applied, when it
// is read (Effective). The same rule binds the per-call overrides of the tool
// (Criteria.Tighten). Nothing here applies a promotion: the checklist only reports.
package promotionsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
)

// Keys of the runtime_settings rows.
const (
	KeyMinCommonRows     = "research.promotion.min_common_rows"
	KeyMinDays           = "research.promotion.min_days"
	KeyMinDiffCILowerBps = "research.promotion.min_diff_ci_lower_bps"
	KeyMinPositiveDayPct = "research.promotion.min_positive_day_pct"
	KeyMaxAdjustedP      = "research.promotion.max_adjusted_p"
)

// Floor values: the documented criteria. A setting may only be stricter.
const (
	FloorMinCommonRows     = 300
	FloorMinDays           = 10
	FloorMinDiffCILowerBps = 0.0
	FloorMinPositiveDayPct = 50.0
	FloorMaxAdjustedP      = 0.05

	// Hard bounds of what a setting can hold (an absurdly large minimum is a typo, not a criterion).
	maxMinCommonRows     = 10_000_000
	maxMinDays           = 3650
	maxMinDiffCILowerBps = 1000.0
	maxMinPositiveDayPct = 100.0
	minMaxAdjustedP      = 1e-12
)

// Keys returns every runtime_settings key of this package.
func Keys() []string {
	return []string{KeyMinCommonRows, KeyMinDays, KeyMinDiffCILowerBps, KeyMinPositiveDayPct, KeyMaxAdjustedP}
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

// IsIntegerKey reports whether key holds a whole number.
func IsIntegerKey(key string) bool { return key == KeyMinCommonRows || key == KeyMinDays }

// DefaultText is the value a key shows while nothing is stored: its floor.
func DefaultText(key string) string {
	switch key {
	case KeyMinCommonRows:
		return fmt.Sprint(FloorMinCommonRows)
	case KeyMinDays:
		return fmt.Sprint(FloorMinDays)
	case KeyMinDiffCILowerBps:
		return fmt.Sprint(FloorMinDiffCILowerBps)
	case KeyMinPositiveDayPct:
		return fmt.Sprint(FloorMinPositiveDayPct)
	}
	return fmt.Sprint(FloorMaxAdjustedP)
}

// Criteria are the effective numeric criteria of one checklist run.
type Criteria struct {
	// MinCommonRows is the fewest rows the candidate and the current one must share.
	MinCommonRows int `json:"min_common_rows"`
	// MinDays is the fewest JST days those rows must cover.
	MinDays int `json:"min_days"`
	// MinDiffCILowerBps is the least the lower end of the difference's day-cluster interval may be (bps).
	MinDiffCILowerBps float64 `json:"min_diff_ci_lower_bps"`
	// MinPositiveDayPct is the least share (%) of the days on which the candidate beat the current one.
	MinPositiveDayPct float64 `json:"min_positive_day_pct"`
	// MaxAdjustedP is the most the multiple-comparison-adjusted p-value may be.
	MaxAdjustedP float64 `json:"max_adjusted_p"`
}

// Floor returns the documented criteria.
func Floor() Criteria {
	return Criteria{FloorMinCommonRows, FloorMinDays, FloorMinDiffCILowerBps, FloorMinPositiveDayPct, FloorMaxAdjustedP}
}

// Validate checks the JSON-encoded value of key: a number within the hard bounds and not
// looser than the key's floor.
func Validate(key, jsonValue string) error {
	var n *float64
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil || n == nil || math.IsNaN(*n) || math.IsInf(*n, 0) {
		return fmt.Errorf("promotionsettings: runtime setting %s=%q must be a number", key, jsonValue)
	}
	v := *n
	if IsIntegerKey(key) && v != math.Trunc(v) {
		return fmt.Errorf("promotionsettings: runtime setting %s=%q must be a whole number", key, jsonValue)
	}
	var lo, hi float64
	switch key {
	case KeyMinCommonRows:
		lo, hi = FloorMinCommonRows, maxMinCommonRows
	case KeyMinDays:
		lo, hi = FloorMinDays, maxMinDays
	case KeyMinDiffCILowerBps:
		lo, hi = FloorMinDiffCILowerBps, maxMinDiffCILowerBps
	case KeyMinPositiveDayPct:
		lo, hi = FloorMinPositiveDayPct, maxMinPositiveDayPct
	case KeyMaxAdjustedP:
		lo, hi = minMaxAdjustedP, FloorMaxAdjustedP // a maximum: smaller is stricter
	default:
		return fmt.Errorf("promotionsettings: %s is not a promotion criterion", key)
	}
	if v < lo || v > hi {
		return fmt.Errorf("promotionsettings: runtime setting %s=%q must be %v〜%v (the criteria can only be made stricter than %s)", key, jsonValue, lo, hi, DefaultText(key))
	}
	return nil
}

// Tighten applies an override of one criterion by its key; it fails when the value would
// be looser than the criterion already in force (the per-call overrides of the tool use it).
func (c *Criteria) Tighten(key string, v float64) error {
	cur := *c
	var err error
	switch key {
	case KeyMinCommonRows, KeyMinDays, KeyMinDiffCILowerBps, KeyMinPositiveDayPct, KeyMaxAdjustedP:
		err = Validate(key, fmt.Sprint(v))
	}
	if err != nil {
		return err
	}
	switch {
	case key == KeyMinCommonRows && v >= float64(cur.MinCommonRows):
		c.MinCommonRows = int(v)
	case key == KeyMinDays && v >= float64(cur.MinDays):
		c.MinDays = int(v)
	case key == KeyMinDiffCILowerBps && v >= cur.MinDiffCILowerBps:
		c.MinDiffCILowerBps = v
	case key == KeyMinPositiveDayPct && v >= cur.MinPositiveDayPct:
		c.MinPositiveDayPct = v
	case key == KeyMaxAdjustedP && v <= cur.MaxAdjustedP:
		c.MaxAdjustedP = v
	default:
		return fmt.Errorf("promotionsettings: %s=%v is looser than the criterion in force; the criteria can only be made stricter", key, v)
	}
	return nil
}

// Store is the runtime_settings read access Effective needs.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Effective returns the criteria in force: each stored value that validates, else the
// floor. A stored value that is looser than its floor (or damaged) is logged and ignored.
// Only a failed store read is an error.
func Effective(ctx context.Context, store Store) (Criteria, error) {
	c := Floor()
	for _, key := range Keys() {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Criteria{}, fmt.Errorf("promotionsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Validate(key, raw); err != nil {
			slog.Warn("promotionsettings: ignoring invalid stored criterion; the documented floor applies", "key", key, "error", err)
			continue
		}
		var v float64
		_ = json.Unmarshal([]byte(raw), &v)
		_ = c.Tighten(key, v) // Validate passed, and the floor is the loosest value, so this stores it
	}
	return c, nil
}
