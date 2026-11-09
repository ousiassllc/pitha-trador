// Package independence classifies stored decisions for the evaluation of one
// fixed model (issue #1046): independent, reference-only or excluded, each
// with a reason.
//
// Independent means strictly after the model's train end, calibration end and
// original OOS end, and strictly after the latest LabelEnd of every row the
// model used plus the walk-forward embargo (training.Split, WalkForwardConfig
// .Embargo): the model has seen the market up to there and not beyond. Rows
// before train_start, in the gaps between windows or merely outside the
// windows are NOT independent, and neither is a row for being after the LLM
// cutoff: a row after the cutoff that the model trained, calibrated or tested
// on stays excluded. The original OOS is reference-only (a rescoring of it is
// never the basis of a verdict). Rows of another cohort or Jev version are
// excluded.
package independence

import (
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
)

// Class of a row.
type Class int

// The classes. Excluded is the zero value so an unclassified row is never
// taken as independent.
const (
	Excluded Class = iota
	Reference
	Independent
)

func (c Class) String() string {
	switch c {
	case Independent:
		return "independent"
	case Reference:
		return "reference"
	default:
		return "excluded"
	}
}

// Reasons of a classification.
const (
	ReasonIndependent       = "independent"
	ReasonOriginalOOS       = "original_oos"
	ReasonOOSNotUsed        = "oos_window_row_not_used"
	ReasonCohortMismatch    = "cohort_mismatch"
	ReasonBeforeTrainStart  = "before_train_start"
	ReasonTrainingWindow    = "in_training_or_calibration_window"
	ReasonGapBetweenWindows = "gap_between_windows"
	ReasonNotAfterOOSEnd    = "not_after_oos_end"
	ReasonLabelHorizon      = "within_label_end_plus_embargo"
	ReasonBeforeLLMCutoff   = "before_llm_cutoff"
	ReasonOutsideEvalWindow = "outside_evaluation_window"
)

// Row is a stored decision's identity for the classification. Cohort is the
// cohort / Jev version key (issue #881).
type Row struct {
	ID        int64
	DecidedAt time.Time
	Cohort    string
}

// Config is the model's windows and the cohort the evaluation is for.
type Config struct {
	Windows manifest.Windows
	// Cohort is the expected cohort / Jev version (the manifest's); a row with
	// another cohort is excluded. The check is never off: an empty Cohort
	// matches only rows without a cohort, and the manifest requires one.
	Cohort string
	// OOSUsed, when non-nil, is the set of decision IDs the original OOS
	// actually held (training.Split.OOS). A row inside the OOS window that is
	// not in it was dropped before the model was tested (e.g. its label was not
	// yet complete), so the model never saw it: it is excluded, not reference.
	OOSUsed map[int64]struct{}
}

// Result is the class of a row and why.
type Result struct {
	Class  Class
	Reason string
}

// Classify returns the class of r. The checks run in a fixed order and the
// first that applies decides.
func (c Config) Classify(r Row) Result {
	w := c.Windows
	t := r.DecidedAt
	trained := laterOf(w.TrainEnd, w.CalibrationEnd)
	switch {
	case r.Cohort != c.Cohort:
		return Result{Excluded, ReasonCohortMismatch}
	case t.Before(w.TrainStart):
		return Result{Excluded, ReasonBeforeTrainStart}
	case !t.After(trained):
		return Result{Excluded, ReasonTrainingWindow}
	case t.Before(w.OOSStart):
		return Result{Excluded, ReasonGapBetweenWindows}
	case t.Before(w.OOSEnd):
		if _, used := c.OOSUsed[r.ID]; c.OOSUsed != nil && !used {
			return Result{Excluded, ReasonOOSNotUsed}
		}
		return Result{Reference, ReasonOriginalOOS}
	case !t.After(w.OOSEnd):
		return Result{Excluded, ReasonNotAfterOOSEnd}
	case !t.After(w.LabelEnd.Add(w.Embargo())):
		return Result{Excluded, ReasonLabelHorizon}
	case t.Before(w.LLMCutoff):
		return Result{Excluded, ReasonBeforeLLMCutoff}
	case !w.EvalFrom.IsZero() && t.Before(w.EvalFrom) || !w.EvalTo.IsZero() && !t.Before(w.EvalTo):
		return Result{Excluded, ReasonOutsideEvalWindow}
	}
	return Result{Independent, ReasonIndependent}
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// Summary counts the classified rows.
type Summary struct {
	Independent int            `json:"independent"`
	Reference   int            `json:"reference"`
	Excluded    int            `json:"excluded"`
	ByReason    map[string]int `json:"by_reason"`
}

// Split classifies rows and returns the IDs of the independent and reference
// rows (in input order) with the counts of every class and reason.
func (c Config) Split(rows []Row) (independent, reference []int64, s Summary) {
	s.ByReason = map[string]int{}
	for _, r := range rows {
		res := c.Classify(r)
		s.ByReason[res.Reason]++
		switch res.Class {
		case Independent:
			s.Independent++
			independent = append(independent, r.ID)
		case Reference:
			s.Reference++
			reference = append(reference, r.ID)
		default:
			s.Excluded++
		}
	}
	return independent, reference, s
}

// Describe is a one-line description of the independence boundary for reports:
// the instant after which decisions are independent.
func (c Config) Describe() string {
	w := c.Windows
	after := laterOf(laterOf(w.TrainEnd, w.CalibrationEnd), laterOf(w.OOSEnd, w.LabelEnd.Add(w.Embargo())))
	return fmt.Sprintf("independent = decided strictly after %s (train end, calibration end, original OOS end, and LabelEnd + embargo)", after.Format(time.RFC3339))
}
