package screener

import (
	"math"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Input is one instrument's most recent scan-cycle data, as assembled by
// the Scheduler from Market Data / Feature Engine output, for Fast
// Screener filtering and scoring (functional.md §4.2).
//
// Turnover5mJPY, BreakoutStrength and VolatilityExpansion are not part of
// domain.Snapshot/domain.Feature: Turnover5mJPY is a trailing 5-minute
// aggregate (Snapshot.Turnover is the raw per-bar/cumulative-session
// value reported by kabuステーションAPI), and BreakoutStrength /
// VolatilityExpansion are not yet computed by Feature Engine. They are
// supplied here as opaque inputs that a later sub-scope wires from
// Market Data / Feature Engine; BreakoutStrength and VolatilityExpansion
// are nil until that happens, which - like Feature's other nullable
// fields (FR-FE-2) - means "excluded from screen_score", not "zero".
type Input struct {
	InstrumentID int64
	Symbol       string
	Snapshot     domain.Snapshot

	Turnover5mJPY float64

	BreakoutStrength    *float64
	VolatilityExpansion *float64
}

// PassesFilter reports whether in clears every FR-FS-1 numeric filter in
// cfg. A filter component whose underlying value is missing (nil) fails
// the candidate rather than passing it through: Fast Screener runs before
// any Jev call, and there is no downstream step left to catch a
// stale/incomplete reading (architecture/overview.md §5 "異常時" applies
// the same conservative rule to stale market data).
func PassesFilter(cfg config.FastScreenerConfig, in Input) bool {
	if in.Snapshot.Price < cfg.MinPrice || in.Snapshot.Price > cfg.MaxPrice {
		return false
	}
	if in.Turnover5mJPY < cfg.MinTurnover5mJPY {
		return false
	}
	if in.Snapshot.SpreadBps == nil || *in.Snapshot.SpreadBps > cfg.MaxSpreadBps {
		return false
	}
	if in.Snapshot.Feature.VolumeRatio5m == nil || *in.Snapshot.Feature.VolumeRatio5m < cfg.MinVolumeRatio {
		return false
	}
	if in.Snapshot.Feature.Return5m == nil || math.Abs(*in.Snapshot.Feature.Return5m) < cfg.MinAbsReturn5mPct {
		return false
	}
	if in.Snapshot.Feature.RealizedVol5m == nil || *in.Snapshot.Feature.RealizedVol5m < cfg.MinRealizedVolatility {
		return false
	}
	return true
}

// ScreenScore computes FR-FS-2's weighted score for in. Terms whose
// underlying value is nil (BreakoutStrength, VolatilityExpansion, and -
// per FR-FE-2 - OrderbookImbalance) are dropped from the sum entirely
// rather than treated as zero, so a missing signal neither helps nor
// hurts a candidate relative to a genuinely-zero one.
//
// normalized_volume_ratio uses Feature.VolumeRatio5m directly: Feature
// Engine already computes it as current-vs-trailing-average volume, i.e.
// an already-normalized ratio rather than a raw count.
func ScreenScore(weights config.FastScreenerWeights, in Input) float64 {
	var score float64
	if v := in.Snapshot.Feature.VolumeRatio5m; v != nil {
		score += weights.VolumeRatio * *v
	}
	if v := in.Snapshot.Feature.Return5m; v != nil {
		score += weights.AbsReturn5m * math.Abs(*v)
	}
	if in.BreakoutStrength != nil {
		score += weights.BreakoutStrength * *in.BreakoutStrength
	}
	if v := in.Snapshot.Feature.OrderbookImbalance; v != nil {
		score += weights.OrderbookImbalance * *v
	}
	if in.VolatilityExpansion != nil {
		score += weights.VolatilityExpansion * *in.VolatilityExpansion
	}
	return score
}

// Run applies PassesFilter to every element of inputs, scores the
// survivors with ScreenScore, and returns the top cfg.TopN by descending
// score as domain.Candidate values ready for Jev Scout / the Scanner
// Dashboard (FR-FS-1, FR-FS-2). Ties keep inputs' relative order
// (stable sort). Candidate.Jev*/CurrentPosition fields are left nil: Jev
// Scout/Trader (functional.md §4.4/§4.5) and Execution/positions are
// later sub-scopes that populate them once a candidate reaches those
// stages.
func Run(cfg config.FastScreenerConfig, inputs []Input) []domain.Candidate {
	passed := make([]domain.Candidate, 0, len(inputs))
	for _, in := range inputs {
		if !PassesFilter(cfg, in) {
			continue
		}
		passed = append(passed, domain.Candidate{
			InstrumentID:   in.InstrumentID,
			Symbol:         in.Symbol,
			Price:          in.Snapshot.Price,
			Return1m:       in.Snapshot.Feature.Return1m,
			Return5m:       in.Snapshot.Feature.Return5m,
			VolumeRatio5m:  in.Snapshot.Feature.VolumeRatio5m,
			PriceVsVWAPBps: in.Snapshot.Feature.PriceVsVWAPBps,
			SpreadBps:      in.Snapshot.SpreadBps,
			ScreenScore:    ScreenScore(cfg.Weights, in),
			AsOf:           in.Snapshot.Timestamp,
		})
	}

	sort.SliceStable(passed, func(i, j int) bool {
		return passed[i].ScreenScore > passed[j].ScreenScore
	})

	if len(passed) > cfg.TopN {
		passed = passed[:cfg.TopN]
	}
	return passed
}
