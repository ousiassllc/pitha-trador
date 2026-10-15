package regime

import (
	"math"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/regimesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/marketregime"
)

// Minimum sample sizes. Below them the figure is not computed (nil), which
// classifies as RegimeUnknown / makes the VWAP z-score unavailable, rather than
// resting a decision on a handful of points.
const (
	// minReturns is the fewest 1-minute returns a volatility or autocorrelation
	// is computed from.
	minReturns = 10
	// minVWAPBars is the fewest bars since the open (before the current one)
	// the VWAP deviation's typical size is measured from.
	minVWAPBars = 10

	bpsPerUnit = 10000.0
)

// minuteReturn is the return of one 1-minute step of an index: the last price
// of minute at against the last price of the minute before.
type minuteReturn struct {
	at time.Time
	r  float64
}

// minuteReturns builds idx's consecutive 1-minute returns over [from, to] from
// bars, which are ~60-second samples in any order. Each minute is represented
// by its last bar, and a return exists only between two adjacent minutes: a
// gap (a missed cycle, the lunch break) is never bridged, so the series never
// mixes a 1-minute and a 5-minute return. Bars after to are ignored, so the
// result never looks ahead of the decision (FR-FE-1).
func minuteReturns(bars []domain.Snapshot, from, to time.Time) []minuteReturn {
	last := make(map[time.Time]float64, len(bars))
	latest := make(map[time.Time]time.Time, len(bars))
	for _, b := range bars {
		if b.Timestamp.Before(from) || b.Timestamp.After(to) || b.Price <= 0 {
			continue
		}
		minute := b.Timestamp.Truncate(time.Minute)
		if prev, ok := latest[minute]; !ok || b.Timestamp.After(prev) {
			latest[minute], last[minute] = b.Timestamp, b.Price
		}
	}
	minutes := make([]time.Time, 0, len(last))
	for m := range last {
		minutes = append(minutes, m)
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i].Before(minutes[j]) })

	var out []minuteReturn
	for i := 1; i < len(minutes); i++ {
		if minutes[i].Sub(minutes[i-1]) != time.Minute {
			continue
		}
		out = append(out, minuteReturn{at: minutes[i], r: last[minutes[i]]/last[minutes[i-1]] - 1})
	}
	return out
}

// volatilityBps is the standard deviation of the returns in bps; ok is false
// below minReturns.
func volatilityBps(rs []minuteReturn) (bps float64, ok bool) {
	if len(rs) < minReturns {
		return 0, false
	}
	mean := meanReturn(rs)
	var ss float64
	for _, r := range rs {
		d := r.r - mean
		ss += d * d
	}
	return math.Sqrt(ss/float64(len(rs))) * bpsPerUnit, true
}

// autocorr is the lag-1 autocorrelation of the returns: the covariance of each
// return with the one of the next minute (adjacent pairs only) over the
// variance, -1..1. ok is false below minReturns returns, with fewer than half
// as many adjacent pairs, or when every return is the same (no variance).
func autocorr(rs []minuteReturn) (ac float64, ok bool) {
	if len(rs) < minReturns {
		return 0, false
	}
	mean := meanReturn(rs)
	var cov, variance float64
	pairs := 0
	for i, r := range rs {
		d := r.r - mean
		variance += d * d
		if i+1 < len(rs) && rs[i+1].at.Sub(r.at) == time.Minute {
			cov += d * (rs[i+1].r - mean)
			pairs++
		}
	}
	if pairs < minReturns/2 || variance == 0 {
		return 0, false
	}
	return math.Max(-1, math.Min(1, cov/variance)), true
}

func meanReturn(rs []minuteReturn) float64 {
	var sum float64
	for _, r := range rs {
		sum += r.r
	}
	return sum / float64(len(rs))
}

// Classify maps the index's 1-minute return volatility (bps) and lag-1
// autocorrelation to a domain.Regime* value: a positive autocorrelation at or
// above the threshold is a trend, a negative one at or below minus the
// threshold is mean reversion; a missing figure, a market quieter than the
// volatility floor or a correlation inside the band is unknown.
func Classify(volatility, autocorrelation *float64, s regimesettings.Settings) string {
	if volatility == nil || autocorrelation == nil || *volatility < s.MinVolatilityBps {
		return marketregime.Unknown
	}
	switch {
	case *autocorrelation >= s.AutocorrThreshold:
		return marketregime.Trend
	case *autocorrelation <= -s.AutocorrThreshold:
		return marketregime.MeanRevert
	}
	return marketregime.Unknown
}

// VWAPZ is the stock's deviation from VWAP at its latest bar, in units of its
// own typical deviation since the open: the latest bar's PriceVsVWAPBps over
// the root-mean-square of the PriceVsVWAPBps of the earlier bars of the session
// (bars at or after open, before the latest). Positive means above VWAP. bars
// are in any order and may hold bars before open or after the latest, which
// are ignored. ok is false with fewer than minVWAPBars earlier bars, when the
// latest bar has no VWAP, or when the earlier bars never left VWAP.
func VWAPZ(bars []domain.Snapshot, open, at time.Time) (z float64, ok bool) {
	var latest *domain.Snapshot
	for i := range bars {
		b := &bars[i]
		if b.Timestamp.Before(open) || b.Timestamp.After(at) {
			continue
		}
		if latest == nil || b.Timestamp.After(latest.Timestamp) {
			latest = b
		}
	}
	if latest == nil || latest.Feature.VWAP <= 0 {
		return 0, false
	}
	var sumSq float64
	n := 0
	for _, b := range bars {
		if b.Timestamp.Before(open) || !b.Timestamp.Before(latest.Timestamp) || b.Feature.VWAP <= 0 {
			continue
		}
		sumSq += b.Feature.PriceVsVWAPBps * b.Feature.PriceVsVWAPBps
		n++
	}
	if n < minVWAPBars {
		return 0, false
	}
	rms := math.Sqrt(sumSq / float64(n))
	if rms == 0 {
		return 0, false
	}
	return latest.Feature.PriceVsVWAPBps / rms, true
}

// Arm assigns a decision to an A/B arm: always the momentum arm while the
// comparison is off; otherwise an even, deterministic split on the decision
// bar (the same instrument and bar always land in the same arm, so a replay
// reproduces it).
func Arm(abEnabled bool, instrumentID int64, at time.Time) string {
	if !abEnabled {
		return marketregime.ArmMomentum
	}
	// A splitmix-style mix of the two inputs: consecutive minutes and
	// consecutive ids must not alternate in lock step.
	x := uint64(instrumentID)*0x9E3779B97F4A7C15 ^ uint64(at.Unix()/60)*0xBF58476D1CE4E5B9 //nolint:gosec // G115: a bit mix; wrap-around of negative inputs is harmless
	x ^= x >> 31
	x *= 0x94D049BB133111EB
	x ^= x >> 29
	if x&1 == 0 {
		return marketregime.ArmMomentum
	}
	return marketregime.ArmRegime
}
