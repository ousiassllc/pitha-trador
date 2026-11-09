package runner

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/replaylayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/report"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// toSection maps the replay layer's result onto the report's plain section. The
// differences are the daily differences (To - From) of the comparison's basis
// stream as a ratio of the segment's initial capital, evaluated by a bootstrap
// over days (stats.DailyDifference): never by decision id. The four primary
// pairs get a verdict; the diagonals are descriptive only.
func toSection(res replaylayer.Result, plan stats.Plan, m manifest.Manifest) (report.ReplaySection, error) {
	sec := report.ReplaySection{Available: true, UnavailablePeriods: []report.UnavailablePeriod{}}
	for _, u := range res.Unavailable {
		sec.UnavailablePeriods = append(sec.UnavailablePeriods, report.UnavailablePeriod{From: dayOf(u.From), To: lastDayOf(u.To), Reason: u.Reason})
	}
	for si, seg := range res.Segments {
		rs := report.ReplaySegment{
			Name: fmt.Sprintf("seg%d", si+1), From: dayOf(seg.From), To: lastDayOf(seg.To), Days: len(seg.Days),
			InitialState: manifest.InitialState{CapitalJPY: seg.InitialCapitalJPY, ConsecutiveLosses: m.Replay.InitialState.ConsecutiveLosses},
		}
		for _, c := range res.Cells {
			rs.Cells = append(rs.Cells, replayCell(c.Name, c.Segments[si], seg.InitialCapitalJPY))
		}
		for _, cmp := range res.Comparisons {
			r, err := difference(plan, cmp, res, si, len(res.Segments) > 1)
			if err != nil {
				return report.ReplaySection{}, err
			}
			rs.Differences = append(rs.Differences, r)
		}
		sec.Segments = append(sec.Segments, rs)
	}
	return sec, nil
}

func dayOf(t time.Time) string { return t.In(marketcalendar.JST).Format("2006-01-02") }

// lastDayOf is the last day of the half-open period ending at to.
func lastDayOf(to time.Time) string { return dayOf(to.Add(-time.Nanosecond)) }

func replayCell(name string, cs replaylayer.CellSegment, capital float64) report.ReplayCell {
	c := report.ReplayCell{
		Name: name, EligibleCandidates: cs.Counts.Eligible, Entries: cs.Counts.Entries, PnLJPY: cs.Counts.NetPnLJPY,
		EndEquityJPY: capital, MaxDrawdownJPY: cs.Drawdown.MaxJPY, MaxDrawdownRatio: cs.Drawdown.MaxPct / 100,
	}
	if capital > 0 {
		c.ReturnOnCapital = cs.Counts.NetPnLJPY / capital
	}
	if n := len(cs.Daily); n > 0 {
		c.EndEquityJPY = cs.Daily[n-1].EquityJPY
		d := report.DailySummary{Days: n, BestDayJPY: cs.Daily[0].PnLJPY, WorstDayJPY: cs.Daily[0].PnLJPY}
		var sum float64
		for _, p := range cs.Daily {
			sum += p.PnLJPY
			d.BestDayJPY, d.WorstDayJPY = max(d.BestDayJPY, p.PnLJPY), min(d.WorstDayJPY, p.PnLJPY)
			if p.PnLJPY > 0 {
				d.PositiveDays++
			}
		}
		d.MeanJPY = sum / float64(n)
		c.Daily = d
	}
	byStage := map[string]map[string]int{}
	for key, n := range cs.DropReasons {
		stage, reason, _ := strings.Cut(key, ":")
		if byStage[stage] == nil {
			byStage[stage] = map[string]int{}
		}
		byStage[stage][reason] += n
	}
	for _, st := range cs.Stages {
		c.Stages = append(c.Stages, eligibility.StageDrop{Stage: st.Stage, Count: st.Dropped, Reasons: byStage[st.Stage]})
	}
	return c
}

// difference is one comparison's daily difference on segment si.
func difference(plan stats.Plan, cmp replaylayer.ComparisonResult, res replaylayer.Result, si int, multi bool) (stats.Result, error) {
	var series *replaylayer.DiffSeries
	for i := range cmp.Segments[si].Series {
		s := &cmp.Segments[si].Series[i]
		if s.Stream == cmp.BasisStream && s.Unit == replaylayer.UnitCapitalRatio {
			series = s
		}
	}
	if series == nil {
		return stats.Result{}, fmt.Errorf("runner: comparison %s has no %s series in %s", cmp.Name, cmp.BasisStream, replaylayer.UnitCapitalRatio)
	}
	from, to := make([]stats.DayValue, len(series.Days)), make([]stats.DayValue, len(series.Days))
	for i, d := range series.Days {
		from[i], to[i] = stats.DayValue{Day: d.Day, Value: d.A}, stats.DayValue{Day: d.Day, Value: d.B}
	}
	a, b := cellSegment(res, cmp.From.Name(), si), cellSegment(res, cmp.To.Name(), si)
	counts := stats.Counts{
		EligibleCandidates: b.Counts.Eligible, TakesFrom: a.Counts.Takes, TakesTo: b.Counts.Takes,
		EntriesFrom: a.Counts.Entries, EntriesTo: b.Counts.Entries,
		Basis: min(a.Counts.Entries, b.Counts.Entries), BasisName: "entries_smaller_cell",
	}
	name := cmp.Name
	if multi {
		name = fmt.Sprintf("%s#seg%d", name, si+1)
	}
	return stats.DailyDifference(plan, name, !cmp.Primary, from, to, counts)
}

func cellSegment(res replaylayer.Result, name string, si int) replaylayer.CellSegment {
	i := slices.IndexFunc(res.Cells, func(c replaylayer.CellResult) bool { return c.Name == name })
	return res.Cells[i].Segments[si]
}
