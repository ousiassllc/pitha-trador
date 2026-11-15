package evaldiag

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// cohortKey identifies a (model_id, question_version) cohort.
type cohortKey struct{ model, version string }

// cohortAcc collects one cohort's decisions while the period is read.
type cohortAcc struct {
	key              cohortKey
	firstAt, lastAt  time.Time
	decisions        int
	beforeCutoff     int
	reasons          map[string]int
	storedAll        storedAcc
	storedEvaluated  storedAcc
	rows             []evalRow
	recomputedSum    float64
	recomputedLabels int
	maxAbsDiff       float64
}

// storedAcc accumulates StoredStats.
type storedAcc struct {
	decisions, labeled int
	sum                float64
}

func (a *storedAcc) add(stored *float64) {
	a.decisions++
	if stored != nil {
		a.labeled++
		a.sum += *stored
	}
}

func (a storedAcc) stats() StoredStats {
	s := StoredStats{Decisions: a.decisions, WithLabel: a.labeled, WithoutLabel: a.decisions - a.labeled}
	if a.labeled > 0 {
		mean := a.sum / float64(a.labeled)
		s.MeanNetBps = &mean
	}
	return s
}

// Run evaluates the trader LONG/SHORT decisions of [r.From, r.To) in scope of req
// cohort is the cohort's full result block.
func (a *cohortAcc) cohort(horizon int) Cohort {
	c := Cohort{
		ModelID: a.key.model, QuestionVersion: a.key.version,
		FirstDecisionDay: a.firstAt.In(query.JST).Format(time.DateOnly), LastDecisionDay: a.lastAt.In(query.JST).Format(time.DateOnly),
		Decisions: a.decisions, ExcludedBeforeCutoff: a.beforeCutoff, Evaluated: len(a.rows),
		ExcludedByReason: []Bucket{},
		StoredLabel: StoredLabel{
			LabelVersion: domain.CalibrationLabelVersionTripleBarrier, HorizonMinutes: horizon,
			AllDecisions: a.storedAll.stats(), EvaluatedRows: a.storedEvaluated.stats(),
		},
		Arms: []Arm{}, Differences: []Difference{}, LLMByEntry: []EntrySplit{},
	}
	for _, reason := range []string{PrefixLLM + KindNoEntry, PrefixLLM + KindWindowShort, PrefixOpposite + KindNoEntry, PrefixOpposite + KindWindowShort, ReasonNoBarMomentum} {
		c.ExcludedByReason = append(c.ExcludedByReason, Bucket{Reason: reason, Count: a.reasons[reason]})
	}
	if a.recomputedLabels > 0 {
		mean, maxAbs := a.recomputedSum/float64(a.recomputedLabels), a.maxAbsDiff
		c.StoredLabel.RecomputedMeanOnLabeled, c.StoredLabel.MaxAbsDifferenceBps = &mean, &maxAbs
	}
	subsets := []struct {
		dir string
		set func(*ByDirection) *Metrics
	}{
		{"", func(b *ByDirection) *Metrics { return &b.All }},
		{domain.JevDirectionLong, func(b *ByDirection) *Metrics { return &b.Long }},
		{domain.JevDirectionShort, func(b *ByDirection) *Metrics { return &b.Short }},
	}
	for i, name := range Arms {
		arm := Arm{Arm: name}
		for _, s := range subsets {
			*s.set(&arm.ByDirection) = metricsOf(subsetOf(a.rows, s.dir), func(r evalRow) float64 { return r.net[i] }, true)
		}
		c.Arms = append(c.Arms, arm)
	}
	for i := iRandom; i < numArms; i++ {
		d := Difference{Baseline: Arms[i]}
		for k, dir := range []string{"", domain.JevDirectionLong, domain.JevDirectionShort} {
			diff := diffOf(subsetOf(a.rows, dir), iLLM, i)
			switch k {
			case 0:
				d.All = diff
			case 1:
				d.Long = diff
			default:
				d.Short = diff
			}
		}
		c.Differences = append(c.Differences, d)
	}
	for _, entry := range []string{Entered, Skipped} {
		var rows []evalRow
		for _, r := range a.rows {
			if r.entered == (entry == Entered) {
				rows = append(rows, r)
			}
		}
		split := EntrySplit{Entry: entry}
		for _, s := range subsets {
			*s.set(&split.ByDirection) = metricsOf(subsetOf(rows, s.dir), func(r evalRow) float64 { return r.net[iLLM] }, false)
		}
		c.LLMByEntry = append(c.LLMByEntry, split)
	}
	return c
}
