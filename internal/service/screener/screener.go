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
// Turnover5mJPY, BreakoutStrength and VolatilityExpansion are supplied as
// inputs wired by internal/bootstrap: Turnover5mJPY is the trailing
// 5-minute traded value (a difference of Snapshot.Turnover, kabuステーション
// API's cumulative session value - featureengine.TurnoverOverWindow, 0 when
// unknown), and BreakoutStrength / VolatilityExpansion are derived from
// price history by featureengine.ComputeScreenSignals; BreakoutStrength and VolatilityExpansion
// are nil when history is insufficient to compute them, which - like
// Feature's other nullable fields (FR-FE-2) - means "excluded from
// screen_score", not "zero".
type Input struct {
	InstrumentID int64
	Symbol       string
	Snapshot     domain.Snapshot

	Turnover5mJPY float64
	// TurnoverMissing marks Turnover5mJPY (counted as 0) as unknown
	// (insufficient history) rather than a genuine 0. The filter outcome is
	// unchanged; only the reason reported when the floor is not met differs.
	TurnoverMissing bool

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
	return FilterReasons(cfg, in) == 0
}

// FilterReasons returns every FR-FS-1 filter in cfg that in fails, or the
// empty set if it clears them all. Unlike a short-circuiting check it
// evaluates every filter so the Scanner Dashboard can show all reasons an
// instrument is out (issue #303); a missing value is reported as its
// missing-data reason, not as a threshold failure. It does not allocate.
func FilterReasons(cfg config.FastScreenerConfig, in Input) domain.ScreenReasons {
	var r domain.ScreenReasons
	if in.Snapshot.Price < cfg.MinPrice {
		r = r.Add(domain.ScreenReasonMinPrice)
	}
	if in.Snapshot.Price > cfg.MaxPrice {
		r = r.Add(domain.ScreenReasonMaxPrice)
	}
	if in.Turnover5mJPY < cfg.MinTurnover5mJPY {
		if in.TurnoverMissing {
			r = r.Add(domain.ScreenReasonMissingTurnover)
		} else {
			r = r.Add(domain.ScreenReasonMinTurnover)
		}
	}
	switch {
	case in.Snapshot.SpreadBps == nil:
		r = r.Add(domain.ScreenReasonMissingSpread)
	case *in.Snapshot.SpreadBps > cfg.MaxSpreadBps:
		r = r.Add(domain.ScreenReasonMaxSpread)
	}
	switch {
	case in.Snapshot.Feature.VolumeRatio5m == nil:
		r = r.Add(domain.ScreenReasonMissingVolumeRatio)
	case *in.Snapshot.Feature.VolumeRatio5m < cfg.MinVolumeRatio:
		r = r.Add(domain.ScreenReasonMinVolumeRatio)
	}
	switch {
	case in.Snapshot.Feature.Return5m == nil:
		r = r.Add(domain.ScreenReasonMissingReturn5m)
	// Return5m is a decimal ratio; MinAbsReturn5mPct is in percent.
	case math.Abs(domain.RatioToPercent(*in.Snapshot.Feature.Return5m)) < cfg.MinAbsReturn5mPct:
		r = r.Add(domain.ScreenReasonMinAbsReturn5m)
	}
	switch {
	case in.Snapshot.Feature.RealizedVol5m == nil:
		r = r.Add(domain.ScreenReasonMissingRealizedVol)
	case *in.Snapshot.Feature.RealizedVol5m < cfg.MinRealizedVolatility:
		r = r.Add(domain.ScreenReasonMinRealizedVol)
	}
	return r
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

// Result is Screen's output: the candidates plus, for every input, why it
// is not one.
type Result struct {
	// Candidates is Run's return value: the top cfg.TopN by screen_score.
	Candidates []domain.Candidate
	// Reasons is parallel to the inputs passed to Screen: the empty set
	// for an input that became a candidate, otherwise every reason it did
	// not (FilterReasons, plus domain.ScreenReasonRankedOut for one that
	// cleared the filters but fell below the top-N cut).
	Reasons []domain.ScreenReasons
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
	return Screen(cfg, inputs).Candidates
}

// Screen is Run that also reports each input's exclusion reasons, for the
// Scanner Dashboard's per-symbol view (issue #303). It adds only a
// two-byte-per-input Reasons slice over Run.
func Screen(cfg config.FastScreenerConfig, inputs []Input) Result {
	type ranked struct {
		idx       int
		candidate domain.Candidate
	}
	reasons := make([]domain.ScreenReasons, len(inputs))
	passed := make([]ranked, 0, len(inputs))
	for i, in := range inputs {
		if reasons[i] = FilterReasons(cfg, in); reasons[i] != 0 {
			continue
		}
		passed = append(passed, ranked{i, domain.Candidate{
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
		}})
	}

	sort.SliceStable(passed, func(i, j int) bool {
		return passed[i].candidate.ScreenScore > passed[j].candidate.ScreenScore
	})

	// config.StrategyConfig.Validate rejects top_n < 1 at startup; clamp
	// anyway so a negative value (e.g. a runtime_settings override) cannot
	// panic the slice below.
	topN := max(cfg.TopN, 0)
	if len(passed) > topN {
		for _, p := range passed[topN:] {
			reasons[p.idx] = reasons[p.idx].Add(domain.ScreenReasonRankedOut)
		}
		passed = passed[:topN]
	}
	candidates := make([]domain.Candidate, len(passed))
	for i, p := range passed {
		candidates[i] = p.candidate
	}
	return Result{Candidates: candidates, Reasons: reasons}
}
