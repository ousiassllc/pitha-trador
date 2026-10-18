package training

import (
	"math"
	"math/rand/v2"
	"slices"
	"sort"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// Bootstrap settings of the adoption intervals (issue #780). The documented
// choices (FR-META-7) are: percentile intervals from Resamples draws, Alpha
// one-sided tails (the interval is the 98% two-sided one: 1% per tail keeps the
// false-positive rate of the lower limit within 5% for 5 or more days even with
// strongly correlated same-day outcomes, which 2.5% does not), and a two-stage
// cluster resampling that respects the correlation of same-day and same-symbol
// decisions.
const (
	DefaultResamples = 2000
	DefaultAlpha     = 0.01
)

// scoredRow is one OOS row a model scored, with the keys the bootstrap
// resamples by.
type scoredRow struct {
	// index is the row's position among the OOS rows, to pair two models.
	index int
	// day (JST date) and symbol key the clusters: decisions of one symbol on
	// one day share the price path (a label window reaches 60 minutes) and
	// all symbols of a day share the market's mood.
	day, symbol string
	win         bool
	raw         float64
	take        bool
	ret         float64
}

func dayOf(r Row) string { return r.DecidedAt.In(marketcalendar.JST).Format("2006-01-02") }

// clusters indexes scored rows into (day, symbol) clusters grouped by day.
type clusters struct {
	// byDay[d] lists the cluster ids of day d.
	byDay [][]int
	n     int
	// of[i] is the cluster id of the i-th scored row.
	of []int
}

func newClusters(rows []scoredRow) clusters {
	type key struct{ day, symbol string }
	var (
		c      clusters
		ids    = map[key]int{}
		dayIdx = map[string]int{}
	)
	c.of = make([]int, len(rows))
	for i, r := range rows {
		k := key{r.day, r.symbol}
		id, ok := ids[k]
		if !ok {
			id = c.n
			c.n++
			ids[k] = id
			d, seen := dayIdx[r.day]
			if !seen {
				d = len(c.byDay)
				dayIdx[r.day] = d
				c.byDay = append(c.byDay, nil)
			}
			c.byDay[d] = append(c.byDay[d], id)
		}
		c.of[i] = id
	}
	return c
}

// days is the number of resampling days (the independent units).
func (c clusters) days() int { return len(c.byDay) }

// resample fills mult with the cluster multiplicities of one two-stage draw:
// pick the days with replacement, then within each picked day pick that
// day's clusters with replacement. The second stage double counts the
// within-day variance, which makes the interval wider (more conservative) when
// there are few days.
func (c clusters) resample(rng *rand.Rand, mult []float64) {
	clear(mult)
	for range c.byDay {
		ids := c.byDay[rng.IntN(len(c.byDay))]
		for range ids {
			mult[ids[rng.IntN(len(ids))]]++
		}
	}
}

// ratioInterval is the percentile interval of sum(mult*num)/sum(mult*den)
// over the resamples; draws with an empty denominator are skipped. It is nil
// when no draw is defined or there are fewer than two days.
func ratioInterval(c clusters, num, den []float64, resamples int, alpha float64, rng *rand.Rand) *repo.Interval {
	if c.days() < 2 {
		return nil
	}
	mult := make([]float64, c.n)
	draws := make([]float64, 0, resamples)
	for range resamples {
		c.resample(rng, mult)
		var n, d float64
		for id, m := range mult {
			n += m * num[id]
			d += m * den[id]
		}
		if d > 0 {
			draws = append(draws, n/d)
		}
	}
	return percentileInterval(draws, alpha)
}

// aucInterval is the percentile interval of the weighted AUC over the cluster
// resamples. rows must be the scored rows c was built from.
func aucInterval(c clusters, rows []scoredRow, resamples int, alpha float64, rng *rand.Rand) *repo.Interval {
	if c.days() < 2 {
		return nil
	}
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return rows[order[a]].raw < rows[order[b]].raw })
	// groupEnd[g] is the end (exclusive) in order of each run of tied scores.
	var groupEnd []int
	for i := 0; i < len(order); {
		j := i
		for j < len(order) && rows[order[j]].raw == rows[order[i]].raw {
			j++
		}
		groupEnd = append(groupEnd, j)
		i = j
	}

	mult := make([]float64, c.n)
	draws := make([]float64, 0, resamples)
	for range resamples {
		c.resample(rng, mult)
		var (
			wins, losses, u float64
			start           int
		)
		for _, end := range groupEnd {
			var gWin, gLose float64
			for _, i := range order[start:end] {
				w := mult[c.of[i]]
				if rows[i].win {
					gWin += w
				} else {
					gLose += w
				}
			}
			u += gWin * (losses + gLose/2)
			wins += gWin
			losses += gLose
			start = end
		}
		if wins > 0 && losses > 0 {
			draws = append(draws, u/(wins*losses))
		}
	}
	return percentileInterval(draws, alpha)
}

// percentileInterval returns the [alpha, 1-alpha] percentiles of draws, or nil
// when there are too few draws to tell (fewer than 20 per tail).
func percentileInterval(draws []float64, alpha float64) *repo.Interval {
	if float64(len(draws))*alpha < 20 {
		return nil
	}
	slices.Sort(draws)
	n := float64(len(draws))
	lo := int(math.Floor(alpha * n))
	hi := int(math.Ceil((1-alpha)*n)) - 1
	return &repo.Interval{Low: draws[lo], High: draws[min(hi, len(draws)-1)]}
}
