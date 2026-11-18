package trenddiag

import (
	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// seriesOf groups the rows (ordered by model_id, question_version, horizon_minutes, day) into
// one series per (model_id, question_version, horizon).
func seriesOf(rows []evalrecord.Row) []Series {
	out := []Series{}
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && sameSeries(rows[i], rows[j]) {
			j++
		}
		out = append(out, buildSeries(rows[i:j]))
		i = j
	}
	return out
}

func sameSeries(a, b evalrecord.Row) bool {
	return a.ModelID == b.ModelID && a.QuestionVersion == b.QuestionVersion && a.HorizonMinutes == b.HorizonMinutes
}

// settingsOf is the label settings a row was evaluated with.
func settingsOf(r evalrecord.Row) LabelSettings {
	return LabelSettings{HorizonMinutes: r.HorizonMinutes, TakeProfitBps: r.TakeProfitBps, StopLossBps: r.StopLossBps, BarrierCostBps: r.BarrierCostBps}
}

// buildSeries is one series from its rows (ascending days).
func buildSeries(rows []evalrecord.Row) Series {
	s := Series{
		ModelID: rows[0].ModelID, QuestionVersion: rows[0].QuestionVersion, HorizonMinutes: rows[0].HorizonMinutes,
		FirstDay: rows[0].Day, LastDay: rows[len(rows)-1].Day, DaysRecorded: len(rows),
		LabelSettingsChangedOn: []string{}, Daily: make([]DayRow, 0, len(rows)),
	}
	llm := make([]evaldiag.DayTotals, 0, len(rows))
	diff := make([]evaldiag.DayTotals, 0, len(rows))
	for i, r := range rows {
		if i > 0 && settingsOf(r) != settingsOf(rows[i-1]) {
			s.LabelSettingsChanged = true
			s.LabelSettingsChangedOn = append(s.LabelSettingsChangedOn, r.Day)
		}
		s.Decisions += r.Decisions
		s.Evaluated += r.Evaluated
		s.Daily = append(s.Daily, dayRow(r))
		llm = append(llm, evaldiag.DayTotals{Day: r.Day, N: r.Evaluated, Sum: r.LLMSum, Wins: r.Wins})
		diff = append(diff, evaldiag.DayTotals{Day: r.Day, N: r.Evaluated, Sum: r.LLMSum - r.RandomSum})
	}
	s.Excluded = s.Decisions - s.Evaluated
	m := evaldiag.SummarizeDays(llm).Metrics
	m.Daily = nil // the per-day rows are Daily above
	d := evaldiag.SummarizeDays(diff)
	s.Cumulative = Cumulative{
		LLM:            m,
		LLMMinusRandom: evaldiag.Diff{N: d.N, Days: d.Days, MeanDiffBps: d.MeanNetBps, CI: d.CI, DetectableMinimumDifferenceBps: d.DetectableMinimumEdgeBps, DaysToDetect: d.DaysToDetect},
	}
	return s
}

// dayRow is one recorded day: the means are null while nothing was evaluated that day.
func dayRow(r evalrecord.Row) DayRow {
	d := DayRow{
		Day: r.Day, Decisions: r.Decisions, Evaluated: r.Evaluated, Excluded: r.Excluded(), Entered: r.Entered,
		LabelSettings: settingsOf(r),
	}
	if r.Evaluated == 0 {
		return d
	}
	n := float64(r.Evaluated)
	rate, llm, random, momentum, contrarian, diff := float64(r.Wins)/n, r.LLMSum/n, r.RandomSum/n, r.MomentumSum/n, r.ContrarianSum/n, (r.LLMSum-r.RandomSum)/n
	d.WinRate, d.LLMMeanNetBps, d.RandomMeanNetBps, d.MomentumMeanNetBps, d.ContrarianMeanNetBps, d.LLMMinusRandomMeanBps = &rate, &llm, &random, &momentum, &contrarian, &diff
	return d
}
