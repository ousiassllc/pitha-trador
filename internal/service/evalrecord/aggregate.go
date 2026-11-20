package evalrecord

import (
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/repository/evalrecord"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
)

// Aggregate turns the evaluated rows of one JST day (evaldiag.Rows over [day, day+1))
// into the day's jev_evaluation_daily rows, one per cohort in meta.Cohorts: the cohort's
// decisions and evaluated counts come from the evaluation itself (so a decision that was
// excluded - no quote, short window, before the training cutoff - is counted in
// Decisions but not in Evaluated), the entered / wins counts and the net bps sums from
// the evaluated rows. A cohort with decisions but no evaluated row still gets its row
// (Evaluated 0, sums 0), so a day with decisions always leaves a record.
func Aggregate(day string, rows []evaldiag.Row, meta evaldiag.Meta) ([]evalrecord.Row, error) {
	type key struct{ model, version string }
	index := map[key]*evalrecord.Row{}
	out := make([]evalrecord.Row, 0, len(meta.Cohorts))
	for _, c := range meta.Cohorts {
		out = append(out, evalrecord.Row{
			Day: day, ModelID: c.ModelID, QuestionVersion: c.QuestionVersion, HorizonMinutes: meta.HorizonMinutes,
			TakeProfitBps: meta.TakeProfitBps, StopLossBps: meta.StopLossBps, BarrierCostBps: meta.BarrierCostBps,
			Decisions: c.Decisions, Evaluated: c.Evaluated,
		})
	}
	for i := range out {
		index[key{out[i].ModelID, out[i].QuestionVersion}] = &out[i]
	}
	counted := map[key]int{}
	for _, r := range rows {
		k := key{r.ModelID, r.QuestionVersion}
		x := index[k]
		if x == nil {
			return nil, fmt.Errorf("evalrecord: evaluated row of %s/%s has no cohort in the evaluation's counts", r.ModelID, r.QuestionVersion)
		}
		if r.Day != day {
			return nil, fmt.Errorf("evalrecord: evaluated row of %s belongs to %s, not %s", r.ModelID, r.Day, day)
		}
		counted[k]++
		if r.Entered {
			x.Entered++
		}
		if r.LLM > 0 {
			x.Wins++
		}
		x.LLMSum += r.LLM
		x.RandomSum += r.Random
		x.MomentumSum += r.Momentum
		x.ContrarianSum += r.Contrarian
	}
	for k, x := range index {
		if counted[k] != x.Evaluated {
			return nil, fmt.Errorf("evalrecord: cohort %s/%s has %d evaluated rows but the evaluation counted %d", k.model, k.version, counted[k], x.Evaluated)
		}
	}
	return out, nil
}
