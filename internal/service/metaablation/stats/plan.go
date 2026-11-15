// Package stats is the verdict logic shared by both layers of the metaablation
// comparison (issue #1046): a day-clustered bootstrap of a difference, the
// family-wise correction, the detectable minimum difference at the
// pre-registered power and the three-valued verdict.
//
// It is deliberately independent of the model adoption bar (issue #862): no
// 370 takes, no 11 bps, no MinOOSDays. The minimum days, minimum count, the
// meaningful difference (delta) and the power come only from the manifest's
// pre-registered Minimums, and while any of them is unset every verdict is
// 優劣未確定. Nothing here searches a threshold: the values come in, they are
// never tuned on the evaluation window.
package stats

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

// Verdict is the outcome of a primary comparison To - From.
type Verdict string

// The three verdicts. A point estimate alone never yields a verdict.
const (
	Superior     Verdict = "優位確認"
	Inferior     Verdict = "劣位確認"
	Undetermined Verdict = "優劣未確定"
)

// Plan is the statistics plan of one layer, taken from the frozen manifest.
type Plan struct {
	MinDays, MinCount *int
	// Delta is the layer's smallest meaningful difference (nil = unset).
	Delta *float64
	// Power is the pre-registered power of the detectable minimum difference.
	Power *float64
	// Alpha is the family-wise two-sided level; Correction and NComparisons
	// (every comparison of the study, exploratory ones included) adjust it.
	Alpha        float64
	Correction   string
	NComparisons int
	Resamples    int
	Seed         int64
}

// PlanFrom builds the Plan of a layer (manifest.LayerLabel or LayerReplay)
// from a frozen manifest, refusing one that is not valid and frozen.
func PlanFrom(m manifest.Manifest, layer string) (Plan, error) {
	if err := m.Verify(); err != nil {
		return Plan{}, err
	}
	p := Plan{
		MinDays: m.Minimums.MinDays, MinCount: m.Minimums.MinCount, Power: m.Minimums.Power,
		Alpha: m.Statistics.Alpha, Correction: m.Statistics.Correction, NComparisons: m.Statistics.NumComparisons,
		Resamples: m.Statistics.Resamples, Seed: m.Statistics.Seed,
	}
	switch layer {
	case manifest.LayerLabel:
		p.Delta = m.Minimums.DeltaLabelBps
	case manifest.LayerReplay:
		p.Delta = m.Minimums.DeltaReplayDailyReturn
	default:
		return Plan{}, fmt.Errorf("stats: unknown layer %q", layer)
	}
	return p, nil
}

// Validate reports a plan the bootstrap cannot run with.
func (p Plan) Validate() error {
	switch {
	case !(p.Alpha > 0 && p.Alpha < 1):
		return fmt.Errorf("stats: alpha %v must be in (0, 1)", p.Alpha)
	case p.Resamples < 1000:
		return fmt.Errorf("stats: %d resamples is too few for a percentile interval (need >= 1000)", p.Resamples)
	case p.Correction != manifest.CorrectionBonferroni && p.Correction != manifest.CorrectionNone:
		return fmt.Errorf("stats: unknown correction %q", p.Correction)
	case p.Correction == manifest.CorrectionBonferroni && p.NComparisons < 1:
		return errors.New("stats: bonferroni needs the total number of comparisons")
	}
	return nil
}

// AlphaAdjusted is the per-comparison two-sided level: Alpha / NComparisons
// under Bonferroni, Alpha without correction.
func (p Plan) AlphaAdjusted() float64 {
	if p.Correction == manifest.CorrectionBonferroni && p.NComparisons > 1 {
		return p.Alpha / float64(p.NComparisons)
	}
	return p.Alpha
}

// UnsetFields names the pre-registered values this plan still lacks.
func (p Plan) UnsetFields() []string {
	var unset []string
	if p.MinDays == nil {
		unset = append(unset, "min_days")
	}
	if p.MinCount == nil {
		unset = append(unset, "min_count")
	}
	if p.Delta == nil {
		unset = append(unset, "delta")
	}
	if p.Power == nil {
		unset = append(unset, "power")
	}
	return unset
}

// seedFor derives a per-comparison seed so a comparison's interval does not
// depend on which other comparisons ran or in which order.
func seedFor(seed int64, name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return uint64(seed) ^ h.Sum64() //nolint:gosec // G115: the bits of the seed are what is mixed in
}

// normalQuantile is the inverse of the standard normal CDF for p in (0, 1).
func normalQuantile(p float64) float64 { return math.Sqrt2 * math.Erfinv(2*p-1) }

// DetectableMinimum is the smallest true difference a two-sided test at level
// alpha detects with the given power when the estimate's standard error is se:
// (z_{1-alpha/2} + z_power) * se. The verdict's detectable minimum difference
// and the read-only evaluation's descriptive one are this one function.
func DetectableMinimum(alpha, power, se float64) float64 {
	return (normalQuantile(1-alpha/2) + normalQuantile(power)) * se
}
