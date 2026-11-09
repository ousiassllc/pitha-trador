package eligibility

import (
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// StageDrop is the number of rows whose FIRST rejecting stage is Stage, with
// the policy reasons behind it.
type StageDrop struct {
	Stage   string         `json:"stage"`
	Count   int            `json:"count"`
	Reasons map[string]int `json:"reasons,omitempty"`
}

// Funnel is the stage-by-stage drop count of one Q setting in the live
// application order. A row is counted at the first stage that rejects it; for
// Q on a row the quality cut rejected first has its flow guards unevaluated,
// while Q off evaluates them. The M, Risk and fill stages are appended by the
// layers that have them.
type Funnel struct {
	// Rows is every row given; Unknown the rows that cannot be recomputed
	// (excluded from all four cells) with UnknownReasons.
	Rows           int            `json:"rows"`
	Unknown        int            `json:"unknown"`
	UnknownReasons map[string]int `json:"unknown_reasons,omitempty"`
	// Drops are the stages in live order: decision, labeled, eligibility,
	// quality (Q on only), flow_guards.
	Drops []StageDrop `json:"drops"`
	// ReachedMeta is the number of momentum candidates that passed every
	// stage before M; ReversionReachedMeta the VWAP reversion candidates that
	// did (reported separately, never in a Q difference).
	ReachedMeta          int `json:"reached_meta"`
	ReversionReachedMeta int `json:"reversion_reached_meta"`
}

// PreMetaStages are the stages the funnel counts, in application order.
var PreMetaStages = []metaablation.Stage{
	metaablation.StageDecision, metaablation.StageLabeled, metaablation.StageEligibility,
	metaablation.StageQuality, metaablation.StageFlowGuards,
}

// BuildFunnel counts results for a Q setting.
func BuildFunnel(results []Result, qOn bool) Funnel {
	f := Funnel{Rows: len(results), UnknownReasons: map[string]int{}}
	byStage := map[metaablation.Stage]*StageDrop{}
	for _, s := range PreMetaStages {
		byStage[s] = &StageDrop{Stage: s.String(), Reasons: map[string]int{}}
	}
	for _, r := range results {
		if r.Unknown != "" {
			f.Unknown++
			f.UnknownReasons[r.Unknown]++
			continue
		}
		o := r.Outcome(qOn)
		if o.Stage == 0 {
			if r.Reversion {
				f.ReversionReachedMeta++
			} else {
				f.ReachedMeta++
			}
			continue
		}
		d := byStage[o.Stage]
		d.Count++
		d.Reasons[o.Reason]++
	}
	for _, s := range PreMetaStages {
		d := *byStage[s]
		if len(d.Reasons) == 0 {
			d.Reasons = nil
		}
		f.Drops = append(f.Drops, d)
	}
	if len(f.UnknownReasons) == 0 {
		f.UnknownReasons = nil
	}
	return f
}

// UnknownReasonList returns the unknown reasons sorted, for stable output.
func (f Funnel) UnknownReasonList() []string {
	out := make([]string, 0, len(f.UnknownReasons))
	for r := range f.UnknownReasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
