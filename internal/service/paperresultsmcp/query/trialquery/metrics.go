package trialquery

import (
	"cmp"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
)

// Direction answers (jev_trial_decisions.direction / jev_decisions.direction).
const (
	dirLong  = "LONG"
	dirShort = "SHORT"
	dirNone  = "NONE"
)

// DirectionCounts is one arm's directions over the cohort. Cohort = Answered +
// NoAnswer: a non-ok row (failed, refused, partial) is a decision the arm gave
// no answer to. NoneCount is the NONE answers, kept as their own bucket.
type DirectionCounts struct {
	Cohort    int `json:"cohort"`
	Answered  int `json:"answered"`
	NoAnswer  int `json:"no_answer"`
	Long      int `json:"long"`
	Short     int `json:"short"`
	NoneCount int `json:"none_count"`
}

// Agreement is how often the arm's direction equals the live direction (NONE
// equals NONE). Over the cohort a no-answer is a disagreement.
type Agreement struct {
	Agree        int      `json:"agree"`
	OverCohort   *float64 `json:"over_cohort"`
	OverAnswered *float64 `json:"over_answered"`
}

// QualityCount is one entry_quality level of an arm's answers.
type QualityCount struct {
	EntryQuality string   `json:"entry_quality"`
	Count        int      `json:"count"`
	OverCohort   *float64 `json:"share_over_cohort"`
	OverAnswered *float64 `json:"share_over_answered"`
}

// QualityHit is one entry_quality level's scored answers and hits in a slice:
// the level exists only for answers, so the rate is over scorable answers.
type QualityHit struct {
	EntryQuality string   `json:"entry_quality"`
	Scorable     int      `json:"scorable"`
	Hits         int      `json:"hits"`
	HitRate      *float64 `json:"hit_rate_over_answered"`
}

// OutcomeStats is one arm's result over a pool of live decisions that have an
// outcome record in a slice (a cohort or its common-success subset).
//
// Records is the pool's decisions with a record. Each is exactly one of:
// NoAnswer (the arm's row is not ok), NoneCount (answered NONE: no directional
// outcome, not a hit or a miss), ExcludedDisagreement (a direction the live
// label cannot score, see Scorable) or Scorable. Scorable are the directional
// answers whose end bar does not depend on the live direction: the arm's
// direction equals the live one (its own barriers) or the live one is NONE (the
// horizon's end); future_return is signed by the arm's direction and a hit is
// above HitThresholdPct. As jev_shadow_compare, the label belongs to the live
// direction, so an opposite answer to a directional live decision is not scored.
type OutcomeStats struct {
	Records              int          `json:"records"`
	NoAnswer             int          `json:"no_answer"`
	NoneCount            int          `json:"none_count"`
	ExcludedDisagreement int          `json:"excluded_disagreement"`
	Scorable             int          `json:"scorable"`
	Hits                 int          `json:"hits"`
	OverCohort           Rate         `json:"hit_rate_over_cohort"`
	OverAnswered         Rate         `json:"hit_rate_over_answered"`
	Coverage             Rate         `json:"coverage"`
	ByEntryQuality       []QualityHit `json:"by_entry_quality"`
}

func armDirection(pool []*decision, arm int) (DirectionCounts, Agreement, []QualityCount) {
	var dc DirectionCounts
	var ag Agreement
	counts := make([]int, len(entryQualityOrder))
	for _, d := range pool {
		dc.Cohort++
		r := d.rows[arm]
		if r.status != jevtrial.StatusOK {
			dc.NoAnswer++
			continue
		}
		dc.Answered++
		switch r.direction {
		case dirLong:
			dc.Long++
		case dirShort:
			dc.Short++
		case dirNone:
			dc.NoneCount++
		}
		if r.direction == d.liveDir {
			ag.Agree++
		}
		if i := slices.Index(entryQualityOrder, r.entryQuality); i >= 0 {
			counts[i]++
		}
	}
	ag.OverCohort, ag.OverAnswered = ratio(ag.Agree, dc.Cohort), ratio(ag.Agree, dc.Answered)
	quality := make([]QualityCount, len(entryQualityOrder))
	for i, eq := range entryQualityOrder {
		quality[i] = QualityCount{EntryQuality: eq, Count: counts[i], OverCohort: ratio(counts[i], dc.Cohort), OverAnswered: ratio(counts[i], dc.Answered)}
	}
	return dc, ag, quality
}

// scoreOutcomes scores arm's answers on the decisions of pool that have a
// record in returns (decision id -> future_return percent).
func scoreOutcomes(pool []*decision, arm int, returns map[int64]float64) OutcomeStats {
	var s OutcomeStats
	scorable := make([]int, len(entryQualityOrder))
	hits := make([]int, len(entryQualityOrder))
	for _, d := range pool {
		fr, ok := returns[d.id]
		if !ok {
			continue
		}
		s.Records++
		r := d.rows[arm]
		switch {
		case r.status != jevtrial.StatusOK:
			s.NoAnswer++
		case r.direction == dirNone:
			s.NoneCount++
		case r.direction != d.liveDir && d.liveDir != dirNone:
			s.ExcludedDisagreement++
		default:
			s.Scorable++
			signed := fr
			if r.direction == dirShort {
				signed = -fr
			}
			i := slices.Index(entryQualityOrder, r.entryQuality)
			if i >= 0 {
				scorable[i]++
			}
			if signed > HitThresholdPct {
				s.Hits++
				if i >= 0 {
					hits[i]++
				}
			}
		}
	}
	s.OverCohort, s.OverAnswered, s.Coverage = rate(s.Hits, s.Records), rate(s.Hits, s.Scorable), rate(s.Scorable, s.Records)
	s.ByEntryQuality = make([]QualityHit, len(entryQualityOrder))
	for i, eq := range entryQualityOrder {
		s.ByEntryQuality[i] = QualityHit{EntryQuality: eq, Scorable: scorable[i], Hits: hits[i], HitRate: ratio(hits[i], scorable[i])}
	}
	return s
}

// LiveBaseline is the live Jev's score of the cohort decisions that have an
// outcome record: its own direction scored by the same rule as an arm's
// (future_return signed by the direction, a hit above HitThresholdPct); a NONE
// is counted, not scored.
type LiveBaseline struct {
	Records    int  `json:"records"`
	NoneCount  int  `json:"none_count"`
	Scorable   int  `json:"scorable"`
	Hits       int  `json:"hits"`
	OverCohort Rate `json:"hit_rate_over_cohort"`
}

func scoreLive(pool []*decision, returns map[int64]float64) LiveBaseline {
	var s LiveBaseline
	for _, d := range pool {
		fr, ok := returns[d.id]
		if !ok {
			continue
		}
		s.Records++
		switch d.liveDir {
		case dirNone:
			s.NoneCount++
		case dirLong, dirShort:
			s.Scorable++
			signed := fr
			if d.liveDir == dirShort {
				signed = -fr
			}
			if signed > HitThresholdPct {
				s.Hits++
			}
		}
	}
	s.OverCohort = rate(s.Hits, s.Records)
	return s
}

// rankByHitRateOverCohort gives the arms a competition rank (1, 1, 3) by their
// hit rate over the cohort, highest first. It is an ordering of one metric: nil
// ranks (below MinOutcomeRecordsForRank records, or with a single arm: a lone rank 1 would read as a winner) when there is too little to order.
func rankByHitRateOverCohort(stats []OutcomeStats) []*int {
	ranks := make([]*int, len(stats))
	if len(stats) < 2 || stats[0].Records < MinOutcomeRecordsForRank {
		return ranks
	}
	order := make([]int, len(stats))
	for i := range order {
		order[i] = i
	}
	value := func(i int) float64 { return *stats[i].OverCohort.Rate }
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(value(b), value(a)) })
	for pos, i := range order {
		r := pos + 1
		if pos > 0 && value(i) == value(order[pos-1]) {
			r = *ranks[order[pos-1]]
		}
		ranks[i] = &r
	}
	return ranks
}
