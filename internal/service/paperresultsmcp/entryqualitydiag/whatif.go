package entryqualitydiag

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

const (
	// noSignal is the pre-guard reason of a decision without a trade_signals row.
	noSignal = "no_signal"
	// unknownQuality is the distribution key of a missing or unrecognised answer.
	unknownQuality = "unknown"
)

// beforeGuards are the reject_reason heads of the gates the Policy Engine
// applies before entry_quality (direction.go's decision tree, regime.go,
// meta.go): a decision with one of them never reached the entry guards, so a
// different min_entry_quality would not have changed its fate.
// probability_below_threshold is the legacy (Paper bootstrap) rule's check
// that runs ahead of entry_quality.
var beforeGuards = map[string]bool{
	policy.ReasonAPIError:                  true,
	policy.ReasonMissingData:               true,
	policy.ReasonNotCalibrated:             true,
	policy.ReasonSpreadTooWide:             true,
	policy.ReasonThinLiquidity:             true,
	policy.ReasonSpecialQuote:              true,
	policy.ReasonPriceLimit:                true,
	policy.ReasonJevNone:                   true,
	policy.ReasonMetaModelUnavailable:      true,
	policy.ReasonProbabilityBelowThreshold: true,
	policy.ReasonVWAPZUnavailable:          true,
	policy.ReasonVWAPNotExtreme:            true,
}

// decisionRow is one stored LONG/SHORT trader decision as the guards read it.
type decisionRow struct {
	id            int64
	version       string
	direction     string
	confidence    sql.NullFloat64
	entryQuality  sql.NullString
	continuation  sql.NullFloat64
	toxicFlow     sql.NullFloat64
	liquidityStr  sql.NullFloat64
	abArm, regime sql.NullString
}

// jevDecision rebuilds the answer fields the guards read; ok is false when one
// is missing (the engine's missing_data).
func (r decisionRow) jevDecision() (domain.JevDecision, bool) {
	if !r.confidence.Valid || !r.entryQuality.Valid || !r.continuation.Valid || !r.toxicFlow.Valid || !r.liquidityStr.Valid {
		return domain.JevDecision{}, false
	}
	return domain.JevDecision{
		Direction: &r.direction, EntryQuality: &r.entryQuality.String, Confidence: &r.confidence.Float64,
		ContinuationProbability: &r.continuation.Float64, ToxicFlow: &r.toxicFlow.Float64, LiquidityStressed: &r.liquidityStr.Float64,
	}, true
}

func (r decisionRow) usesVWAPReversion() bool {
	return marketregime.Assessment{ABArm: r.abArm.String, Regime: r.regime.String}.UsesVWAPReversion()
}

// Analyze counts, for each min_entry_quality of a, what the entry guards would
// do with the LONG/SHORT trader decisions of r. It reads only.
func Analyze(ctx context.Context, q query.Q, r query.Range, a Args) (Data, error) {
	levels, err := scenarioLevels(a.MinEntryQuality)
	if err != nil {
		return Data{}, err
	}
	policyCfg, overridden, err := effectiveThresholds(ctx, q, a.Baseline)
	if err != nil {
		return Data{}, err
	}
	from, to := r.Bounds()
	out := Data{
		RowsFrom: from, RowsTo: to,
		PromptVersions: PromptScope{Only: a.PromptVersion, Excluded: append([]string{}, a.Foreign...)},
		Thresholds: Thresholds{
			Long: directionThresholds(policyCfg.Long), Short: directionThresholds(policyCfg.Short), OverriddenKeys: overridden,
		},
		Population: Population{RejectedBeforeGuardsByReason: []Bucket{}},
	}
	if a.PromptVersion != "" {
		out.PromptVersions.Excluded = []string{}
	}
	scope, scopeArgs := versionScope(a)

	if out.Population.TraderDecisions, out.Population.LongShort, err = countDecisions(ctx, q, from, to, scope, scopeArgs); err != nil {
		return Data{}, err
	}
	if out.Population.LongShort > MaxRows {
		return Data{}, query.ErrLimitExceeded
	}
	rows, err := listDecisions(ctx, q, from, to, scope, scopeArgs)
	if err != nil {
		return Data{}, err
	}
	heads, err := signalHeads(ctx, q, from, to)
	if err != nil {
		return Data{}, err
	}

	out.Scenarios = make([]Scenario, len(levels))
	for i, level := range levels {
		out.Scenarios[i].MinEntryQuality = level
	}
	dist := map[[2]string]int{}
	before := map[string]int{}
	passers := make([][]candidate, len(levels))
	for _, row := range rows {
		quality := unknownQuality
		if _, ok := domain.EntryQualityRank(row.entryQuality.String); ok {
			quality = row.entryQuality.String
		}
		dist[[2]string{row.version, quality}]++

		d, complete := row.jevDecision()
		head, hasSignal := heads[row.id]
		switch {
		case row.usesVWAPReversion():
			out.Population.VWAPReversion++
		case hasSignal && head == policy.ReasonNotLendable:
			out.Population.NotLendableShort++
		case !hasSignal:
			out.Population.RejectedBeforeGuards++
			before[noSignal]++
		case beforeGuards[head]:
			out.Population.RejectedBeforeGuards++
			before[head]++
		case !complete:
			out.Population.IncompleteAnswer++
		default:
			out.Population.ReachedGuards++
			for i := range out.Scenarios {
				if judge(&out.Scenarios[i], policyCfg, row.direction, d) {
					passers[i] = append(passers[i], candidate{id: row.id, short: row.direction == domain.JevDirectionShort})
				}
			}
		}
	}
	out.Population.RejectedBeforeGuardsByReason = buckets(before)
	out.Distribution = distribution(dist)

	// The outcome labels only describe the candidates counted above: they never
	// change which decisions pass.
	labels, err := listLabels(ctx, q, from, to, scope, scopeArgs)
	if err != nil {
		return Data{}, err
	}
	for i := range out.Scenarios {
		out.Scenarios[i].Outcomes = ScenarioOutcomes{
			Long:  outcomesOf(passers[i], false, labels),
			Short: outcomesOf(passers[i], true, labels),
		}
	}
	if out.LabelAssumptions, err = labelAssumptions(ctx, q); err != nil {
		return Data{}, err
	}
	return out, nil
}

// judge runs the engine's guard chain on d for scenario s, counts the verdict
// and reports whether the decision passed every guard. Probability is not part
// of it: the live rule replaces it with the meta-labeling model's verdict,
// which a stored answer cannot be re-run through.
func judge(s *Scenario, cfg config.PolicyConfig, direction string, d domain.JevDecision) (passedAll bool) {
	t, side := cfg.Long, &s.Long
	if direction == domain.JevDirectionShort {
		t, side = cfg.Short, &s.Short
	}
	t.MinEntryQuality = s.MinEntryQuality
	reason := policy.ThresholdReject(t, &d, false)
	for _, c := range []*Counts{&s.Total, side} {
		c.Candidates++
		switch head(reason) {
		case "":
			c.PassEntryQuality++
			c.PassAllGuards++
			passedAll = true
		case policy.ReasonEntryQualityBelowThreshold:
			c.BelowEntryQuality++
		case policy.ReasonContinuationProbabilityBelowThreshold:
			c.PassEntryQuality++
			c.BlockedContinuation++
		case policy.ReasonToxicFlowAboveThreshold:
			c.PassEntryQuality++
			c.BlockedToxicFlow++
		case policy.ReasonLiquidityStressedAboveThreshold:
			c.PassEntryQuality++
			c.BlockedLiquidityStressed++
		}
	}
	return passedAll
}

// head is the reject_reason code: the text before the first ':' (or ';'),
// trimmed; "" for no reason.
func head(reason *string) string {
	if reason == nil {
		return ""
	}
	h, _, _ := strings.Cut(*reason, ":")
	h, _, _ = strings.Cut(h, ";")
	return strings.TrimSpace(h)
}

func directionThresholds(t config.PolicyDirectionThresholds) DirectionThresholds {
	return DirectionThresholds{MinContinuationProbability: t.MinContinuationProbability, MaxToxicFlow: t.MaxToxicFlow, MaxLiquidityStressed: t.MaxLiquidityStressed}
}

// scenarioLevels validates the asked min_entry_quality values, drops
// duplicates and orders them from the lowest.
func scenarioLevels(asked []string) ([]string, error) {
	if len(asked) == 0 {
		asked = DefaultScenarios
	}
	if len(asked) > MaxScenarios {
		return nil, fmt.Errorf("entryqualitydiag: %d min_entry_quality values asked, at most %d", len(asked), MaxScenarios)
	}
	var out []string
	for _, level := range asked {
		if _, ok := domain.EntryQualityRank(level); !ok {
			return nil, fmt.Errorf("entryqualitydiag: %q is not an entry quality", level)
		}
		if !slices.Contains(out, level) {
			out = append(out, level)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		ra, _ := domain.EntryQualityRank(a)
		rb, _ := domain.EntryQualityRank(b)
		return ra - rb
	})
	return out, nil
}

func buckets(m map[string]int) []Bucket {
	out := make([]Bucket, 0, len(m))
	for reason, n := range m {
		out = append(out, Bucket{Reason: reason, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// distribution lists the counts by prompt version, then from the worst
// entry_quality to the best ("unknown" last).
func distribution(m map[[2]string]int) []Distribution {
	out := make([]Distribution, 0, len(m))
	for k, n := range m {
		out = append(out, Distribution{PromptVersion: k[0], EntryQuality: k[1], Count: n})
	}
	rank := func(q string) int {
		r, ok := domain.EntryQualityRank(q)
		if !ok {
			return MaxScenarios
		}
		return r
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PromptVersion != out[j].PromptVersion {
			return out[i].PromptVersion < out[j].PromptVersion
		}
		return rank(out[i].EntryQuality) < rank(out[j].EntryQuality)
	})
	return out
}
