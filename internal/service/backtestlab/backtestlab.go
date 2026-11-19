// Package backtestlab runs the research backtests of the Paper results MCP
// (issue #1086; docs/architecture/paper-results-mcp-backtest.md): one period,
// one set of stored RunConfigs, replayed once per parameter set (variant.Params)
// through the real replay engine, with the cost-inclusive result of each set
// summarized for the record.
//
// It only reads. The RunConfigs come from a ConfigSource (in production
// backtestsource.Source over a read-only database) and are never modified: a
// variant is laid over a copy. Nothing here stores anything, changes a setting
// or feeds a result back into trading; the record itself is
// internal/repository/backtestrun, written by the MCP's narrow writer.
package backtestlab

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/variant"
)

// ConfigSource loads the replay input of a period: one RunConfig per active
// instrument. backtestsource.Source satisfies it.
type ConfigSource interface {
	RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error)
}

// Limits of one run. They bound the work a single MCP call can ask for; the
// MCP documents them (docs/api/endpoints/research-tools/paper-results-mcp-backtest.md).
const (
	// MaxVariants is the most parameter sets one run explores.
	MaxVariants = 12
	// MaxExtraCostPoints is the most extra-cost rows of the sensitivity table.
	MaxExtraCostPoints = 6
	// MaxExtraCostBps is the largest extra round-trip cost one row may add.
	MaxExtraCostBps = 200.0
	// MaxLabelRunes is the longest variant label.
	MaxLabelRunes = 40
	// MaxPeriodDays is the longest period of one run.
	MaxPeriodDays = 31
)

// ReferenceCaution is shown with every result: the best of several sets is a
// reference, not a finding (multiple comparisons).
const ReferenceCaution = "多重比較: 最良値は参考。複数の組から選んだ最良値は偶然で良く見える。試した組の数（explored_count）と一緒に読み、採用の根拠にしない。"

// DefaultExtraCostBps returns the extra round-trip costs of the sensitivity
// table when a request names none (sensitivity.DefaultExtraBps).
func DefaultExtraCostBps() []float64 { return []float64{0, 2, 5, 10} }

// Variant is one parameter set of a run. Index is its position in the request
// (the first is 0); Label is for the reader.
type Variant struct {
	Label  string         `json:"label"`
	Params variant.Params `json:"params"`
}

// Request is the validated input of Run apart from the period.
type Request struct {
	Variants     []Variant `json:"variants"`
	ExtraCostBps []float64 `json:"extra_cost_bps"`
}

// ErrTooManyVariants and ErrTooManyExtraCosts are the limit errors of Normalize.
var (
	ErrTooManyVariants   = fmt.Errorf("backtestlab: more than %d parameter sets", MaxVariants)
	ErrTooManyExtraCosts = fmt.Errorf("backtestlab: more than %d extra-cost rows", MaxExtraCostPoints)
)

// InvalidError names the argument that was refused and why. Reason quotes only
// the accepted range, never the caller's value.
type InvalidError struct {
	Field  string
	Reason string
}

func (e *InvalidError) Error() string { return fmt.Sprintf("backtestlab: %s: %s", e.Field, e.Reason) }

// Normalize validates the variants and extra costs of a request and fills the
// defaults: no variants is one baseline set, an empty label is "baseline" for
// a set that overrides nothing and "v<N>" otherwise, no extra costs is
// DefaultExtraCostBps. It returns ErrTooManyVariants / ErrTooManyExtraCosts
// past the limits and *InvalidError for a bad parameter, label or cost.
func Normalize(variants []Variant, extra []float64) (Request, error) {
	if len(variants) > MaxVariants {
		return Request{}, ErrTooManyVariants
	}
	if len(extra) > MaxExtraCostPoints {
		return Request{}, ErrTooManyExtraCosts
	}
	if len(variants) == 0 {
		variants = []Variant{{}}
	}
	out := Request{Variants: make([]Variant, len(variants)), ExtraCostBps: extra}
	for i, v := range variants {
		if utf8.RuneCountInString(v.Label) > MaxLabelRunes {
			return Request{}, &InvalidError{Field: fmt.Sprintf("variants[%d].label", i), Reason: fmt.Sprintf("%d 文字以内です", MaxLabelRunes)}
		}
		if err := v.Params.Validate(); err != nil {
			field, reason := "params", err.Error()
			var ie *variant.InvalidError
			if errors.As(err, &ie) {
				field, reason = ie.Field, ie.Reason
			}
			return Request{}, &InvalidError{Field: fmt.Sprintf("variants[%d].params.%s", i, field), Reason: reason}
		}
		if v.Label == "" {
			v.Label = fmt.Sprintf("v%d", i+1)
			if v.Params.IsBaseline() {
				v.Label = "baseline"
			}
		}
		out.Variants[i] = v
	}
	if len(out.ExtraCostBps) == 0 {
		out.ExtraCostBps = DefaultExtraCostBps()
	}
	for i, c := range out.ExtraCostBps {
		if !(c >= 0 && c <= MaxExtraCostBps) { // also refuses NaN
			return Request{}, &InvalidError{Field: fmt.Sprintf("extra_cost_bps[%d]", i), Reason: fmt.Sprintf("0 以上 %g 以下です", MaxExtraCostBps)}
		}
	}
	return out, nil
}
