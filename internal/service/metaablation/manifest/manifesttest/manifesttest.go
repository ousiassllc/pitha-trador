// Package manifesttest builds a valid synthetic manifest for tests. Its values
// are arbitrary fixtures, not proposals: every Minimums field is left unset.
package manifesttest

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

// T0 is the fixture's reference time (JST midnight, a Monday).
var T0 = time.Date(2026, 10, 19, 0, 0, 0, 0, time.FixedZone("JST", 9*3600))

// Cohort is the fixture's cohort.
const Cohort = "jev-v2"

// Valid returns an unfrozen, valid manifest with all Minimums unset.
func Valid() manifest.Manifest {
	var family []string
	for _, c := range metaablation.Comparisons() {
		if c.Primary {
			family = append(family, c.Name)
		}
	}
	day := 24 * time.Hour
	return manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		CreatedAt:     T0.Add(20 * day),
		Model:         manifest.ModelRef{Version: "lr-20261101-000000", ParamsSHA256: manifest.ParamsHash(`{"synthetic":true}`)},
		DBCopy:        manifest.DBCopyRef{SHA256: manifest.ParamsHash("db-copy"), AsOf: T0.Add(19 * day)},
		CodeVersion:   "test",
		Cohort:        Cohort,
		Windows: manifest.Windows{
			TrainStart: T0, TrainEnd: T0.Add(5 * day), CalibrationEnd: T0.Add(5 * day),
			OOSStart: T0.Add(6 * day), OOSEnd: T0.Add(8 * day), LabelEnd: T0.Add(8*day + 10*time.Minute),
			EmbargoSeconds: 1800, LLMCutoff: T0.Add(-day), EvalFrom: T0.Add(9 * day), EvalTo: T0.Add(19 * day),
		},
		Rules: manifest.Rules{
			QualityCut:  metaablation.QualityCut,
			Eligibility: manifest.EligibilityRules{MaxSpreadBps: 10, MinTurnover5mJPY: 1e6},
			Long:        manifest.FlowGuards{MinContinuationProbability: 0.5, MaxToxicFlow: 0.5, MaxLiquidityStressed: 0.5},
			Short:       manifest.FlowGuards{MinContinuationProbability: 0.5, MaxToxicFlow: 0.5, MaxLiquidityStressed: 0.5},
			MinEVBps:    0,
			Label:       manifest.LabelCost{TPBps: 30, SLBps: 30, HorizonMinutes: 10, CostBps: 5},
			Replay: manifest.ReplayRules{
				InitialCapitalJPY: 1e6, MaxPositionPerSymbolJPY: 3e5, MaxTotalExposureJPY: 9e5, StopLossPct: 0.5,
				FillModel: "fillmodel-test", SpreadBps: 2, SlippageBps: 1, FeeBps: 0,
			},
		},
		Statistics: manifest.Statistics{
			Method: manifest.TestMethod,
			PrimaryMetrics: []manifest.LayerMetric{
				{Layer: manifest.LayerLabel, Metric: "net_return_bps_per_eligible_candidate"},
				{Layer: manifest.LayerReplay, Metric: "daily_return_on_capital"},
			},
			ComparisonFamily: family, NumComparisons: 8, Correction: manifest.CorrectionBonferroni,
			Alpha: 0.05, Resamples: 2000, Seed: 20261109,
		},
		Replay:     manifest.ReplayPlan{InitialState: manifest.InitialState{CapitalJPY: 1e6}, SegmentRule: "split at gaps of market_snapshots; common to all cells"},
		Provenance: manifest.Provenance{ChosenBeforeEvaluation: true, Notes: []string{"synthetic fixture"}},
		Run:        manifest.Run{Count: 1},
	}
}
