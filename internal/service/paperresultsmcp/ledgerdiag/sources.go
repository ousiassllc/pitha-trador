package ledgerdiag

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Where a candidate's cost of one answer comes from.
const (
	// CostReported: the API reported it (trial usage.cost, shadow usage.input_tokens × list price).
	CostReported = "reported"
	// CostListPrice: trial list price × tokens (an estimate).
	CostListPrice = "list_price_estimate"
	// CostUnknown: neither a report nor an estimate exists (trial cost_source=unknown, or a live
	// decision whose request_cost was not recorded). Never 0.
	CostUnknown = "unknown"
	// CostUsageEstimate: a shadow row whose input tokens were estimated from the request size.
	CostUsageEstimate = "usage_estimate"
	// CostRecorded: the live decision's request_cost.
	CostRecorded = "recorded"
	// CostNone: the candidate calls no paid API (baselines, the feature-only research model).
	CostNone = "none"
)

// answer is one candidate's answer on one reference row.
type answer struct {
	dir    string
	usd    float64
	hasUSD bool
	cost   string
}

// source is one candidate (or the live reference) as read: the answers by decision id.
type source struct {
	id, kind, key string
	baseline      string
	answers       map[int64]answer
	// failed marks reference rows the candidate was asked but did not answer (a trial row whose status is not ok).
	failed map[int64]bool
	// seen: the candidate has at least one row in the period (an answer or a failure).
	seen   bool
	models map[string]bool
	// cohort is a live_cohort's own rows (different decisions from the reference's).
	cohort []decRow
}

func newSource(id, kind, key string) *source {
	return &source{id: id, kind: kind, key: key, answers: map[int64]answer{}, failed: map[int64]bool{}, models: map[string]bool{}}
}

func (s *source) addModel(m string) {
	if m != "" {
		s.models[m] = true
	}
}

// modelIDs are the distinct model ids, sorted; a source without a model of its own (a baseline) is its own id.
func (s *source) modelIDs() []string {
	out := make([]string, 0, len(s.models))
	for m := range s.models {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

// answered reports whether the source answered row d (a baseline always does) and its answer.
func (s *source) answered(d decRow) (answer, bool) {
	if s.baseline != "" {
		return answer{cost: CostNone, hasUSD: true}, true
	}
	a, ok := s.answers[d.ID]
	return a, ok
}

// net is the source's net bps on row d, given that it answered.
func (s *source) net(d decRow, a answer) float64 {
	if s.baseline != "" {
		return d.baselineNet(s.baseline)
	}
	return d.dirNet(a.dir)
}

// liveAnswer is the live decision's own answer and its recorded request_cost (NULL is unknown, never 0).
func liveAnswer(d decRow) answer {
	if !d.HasCost {
		return answer{dir: d.Dir, cost: CostUnknown}
	}
	return answer{dir: d.Dir, usd: d.CostUSD, hasUSD: true, cost: CostRecorded}
}

// kindRank orders candidates by kind in a fixed way (never by a value).
func kindRank(kind string) int {
	return slices.Index([]string{KindLive, KindLiveCohort, KindShadow, KindTrial, KindBaseline, KindResearchModel}, kind)
}

// sourceSet holds every candidate found in the period and the live reference's rows.
type sourceSet struct {
	byID    map[string]*source
	refRows []decRow // the live reference's rows (live version, model filter), time order
	rowByID map[int64]decRow
}

func (set *sourceSet) get(id, kind, key string) *source {
	if s := set.byID[id]; s != nil {
		return s
	}
	s := newSource(id, kind, key)
	set.byID[id] = s
	return s
}

// loadSources reads every candidate of the period: the live cohorts other than the reference, the
// shadow versions, the trial arms, the research model's runs, the baselines, and the live reference.
func loadSources(ctx context.Context, q query.Q, r query.Range, ds *dataset, req Request) (*sourceSet, error) {
	set := &sourceSet{byID: map[string]*source{}, rowByID: map[int64]decRow{}}
	live := set.get(LiveID, KindLive, "")
	for _, d := range ds.rows {
		if d.Version == ds.liveVersion && (req.ModelID == "" || d.Model == req.ModelID) {
			set.refRows = append(set.refRows, d)
			set.rowByID[d.ID] = d
			live.seen = true
			live.answers[d.ID] = liveAnswer(d)
			live.addModel(d.Model)
			continue
		}
		cohort := set.get(KindLiveCohort+":"+d.Model+"/"+d.Version, KindLiveCohort, d.Model+"/"+d.Version)
		cohort.seen = true
		cohort.cohort = append(cohort.cohort, d)
		cohort.addModel(d.Model)
	}
	for _, arm := range Baselines {
		b := set.get(KindBaseline+":"+arm, KindBaseline, arm)
		b.baseline, b.seen = arm, true
		b.addModel(KindBaseline + ":" + arm)
	}
	if len(set.refRows) == 0 {
		return set, nil // nothing to answer: shadow, trial and research rows are read for reference rows only
	}
	from, to := r.Bounds()
	if err := set.loadShadow(ctx, q, from, to, ds.liveVersion, req.ModelID); err != nil {
		return nil, err
	}
	if err := set.loadTrial(ctx, q, from, to, ds.liveVersion, req.ModelID); err != nil {
		return nil, err
	}
	return set, set.loadResearchModel(ctx, q, r)
}

// candidates are the sources lined up against the reference, in a fixed kind-then-id order. With no
// explicit list it is every candidate found in the period; an explicit id that was not found is
// listed with no answers.
func (set *sourceSet) candidates(req Request) []*source {
	var out []*source
	if len(req.Candidates) == 0 {
		for _, id := range req.Include {
			kind, key, _ := ParseID(id)
			set.get(id, kind, key)
		}
		for id, s := range set.byID {
			if id != req.reference() && (s.seen || slices.Contains(req.Include, id)) {
				out = append(out, s)
			}
		}
	} else {
		for _, id := range req.Candidates {
			kind, key, _ := ParseID(id)
			s := set.get(id, kind, key)
			if kind == KindBaseline {
				s.baseline = key
			}
			if id != req.reference() {
				out = append(out, s)
			}
		}
	}
	slices.SortFunc(out, func(a, b *source) int {
		if c := kindRank(a.kind) - kindRank(b.kind); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	return out
}
