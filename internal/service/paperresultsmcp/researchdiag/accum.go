package researchdiag

import (
	"database/sql"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// dayRow is one (horizon, side, JST day) group's sums.
type dayRow struct {
	n, withSpread, wins, aboveCost                int
	gross, net, cost, spread, grossLessHalfSpread float64
}

type key struct{ horizon, side string }

// values are the observations of one (horizon, side) the medians are taken over.
type values struct {
	gross, net, cost, spread []float64
}

// acc collects the groups and the observations of a scope.
type acc struct {
	days   map[key]map[string]*dayRow
	values map[key]*values
	// percent is the share of the rows (per horizon) the observations are.
	percent map[string]int
}

func newAcc() *acc {
	return &acc{days: map[key]map[string]*dayRow{}, values: map[key]*values{}, percent: map[string]int{}}
}

func (a *acc) day(k key, day string) *dayRow {
	if a.days[k] == nil {
		a.days[k] = map[string]*dayRow{}
	}
	if a.days[k][day] == nil {
		a.days[k][day] = &dayRow{}
	}
	return a.days[k][day]
}

// observe adds one row of a side to the observations (and, when sums is set, to
// the day's sums: the decision scopes sum the rows themselves, the snapshots
// scope reads the sums from research_label_day_stats).
func (a *acc) observe(k key, day string, gross, net, cost float64, spread sql.NullFloat64, sums bool) {
	if a.values[k] == nil {
		a.values[k] = &values{}
	}
	v := a.values[k]
	v.gross, v.net, v.cost = append(v.gross, gross), append(v.net, net), append(v.cost, cost)
	if spread.Valid {
		v.spread = append(v.spread, spread.Float64)
	}
	if !sums {
		return
	}
	d := a.day(k, day)
	d.n++
	d.gross += gross
	d.net += net
	d.cost += cost
	if net > 0 {
		d.wins++
	}
	if spread.Valid {
		d.withSpread++
		d.spread += spread.Float64
		d.grossLessHalfSpread += gross - spread.Float64/2
		if spread.Float64 > cost {
			d.aboveCost++
		}
	}
}

func (a *acc) total(k key) (t dayRow) {
	for _, d := range a.days[k] {
		t.n += d.n
		t.withSpread += d.withSpread
		t.wins += d.wins
		t.aboveCost += d.aboveCost
		t.gross += d.gross
		t.net += d.net
		t.cost += d.cost
		t.spread += d.spread
		t.grossLessHalfSpread += d.grossLessHalfSpread
	}
	return t
}

func sideStats(a *acc, k key) (SideStats, SideCost) {
	t := a.total(k)
	n := float64(t.n)
	v := a.values[k]
	if v == nil {
		v = &values{}
	}
	stat := SideStats{
		Rows: t.n, Days: len(a.days[k]),
		MeanGrossBps: t.gross / n, MeanNetBps: t.net / n,
		MedianGrossBps: median(v.gross), MedianNetBps: median(v.net), MedianSamplePercent: a.percent[k.horizon],
		WinRate: float64(t.wins) / n,
	}
	days := make([]string, 0, len(a.days[k]))
	for day := range a.days[k] {
		days = append(days, day)
	}
	slices.Sort(days) // a fixed order: the bootstrap stream is the same for the same data
	clusters := make([]stats.Cluster, len(days))
	for i, day := range days {
		d := a.days[k][day]
		clusters[i] = stats.Cluster{Sum: d.net, N: float64(d.n)}
	}
	if est := stats.Bootstrap(clusters, Resamples, Alpha, Seed); est.CI != nil {
		stat.MeanNetCI = &Interval{Lower: est.CI.Lower, Upper: est.CI.Upper}
		se := est.SE
		mde := (normalQuantile(1-Alpha/2) + normalQuantile(Power)) * se
		stat.SEBps, stat.DetectableMinimumBps = &se, &mde
	}
	cost := SideCost{Rows: t.n, AssumedCost: Distribution{MeanBps: t.cost / n, MedianBps: median(v.cost)}, RowsWithSpread: t.withSpread}
	if t.withSpread > 0 && len(v.spread) > 0 {
		w := float64(t.withSpread)
		cost.Spread = &Distribution{MeanBps: t.spread / w, MedianBps: median(v.spread)}
		after, share := t.grossLessHalfSpread/w, float64(t.aboveCost)/w
		cost.NetAfterSpreadBps, cost.ShareSpreadAboveCost = &after, &share
	}
	return stat, cost
}
