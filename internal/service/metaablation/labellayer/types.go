// Package labellayer is table 1 of the metaablation comparison (issue #1046):
// per cell, on the independent rows only, the label-based net bps per eligible
// candidate (a skip counts 0) and per take, and the paired differences
// (To - From) by decision id with a day-clustered bootstrap interval and the
// verdict of package stats.
//
// Population: the eligible candidates of the independent rows (decisions that
// passed the eligibility conditions, common to all cells), labelled with the
// proxy label (triple barrier TP/SL, issue #774), without Risk or capital. It
// is never mixed with population (i) - the original model evaluation, the
// guard-free rescoring of the original OOS, which is reference only and is
// reported separately - nor with the replay layer's entered trades.
//
// The M axis scores with the stored model as it is (metalabel.Decide); nothing
// here trains, calibrates or fits, and the model must be the one frozen in the
// manifest.
package labellayer

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/independence"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
)

// Row is one stored decision of the label layer's population: a LONG/SHORT
// trader decision with the inputs it had, filled by the loader from a DB copy.
type Row struct {
	ID        int64
	DecidedAt time.Time
	// Cohort is the cohort / Jev version key compared with the manifest's cohort.
	Cohort string
	// Direction is Jev's direction (LONG/SHORT), the side of the label.
	Direction string
	// NetReturnBps is the label's cost-adjusted net return; valid when
	// Eligibility.Labeled.
	NetReturnBps float64
	Eligibility  eligibility.Input
}

// CellResult is one cell of table 1.
type CellResult struct {
	Cell metaablation.Cell `json:"-"`
	Name string            `json:"name"`
	// Funnel is the drop count per stage in the live order, up to the M stage
	// (eligibility package), followed by the M stage of this cell.
	Funnel eligibility.Funnel `json:"funnel"`
	// MetaSkips are the candidates the model skipped (M on only).
	MetaSkips int `json:"meta_skips"`
	// Days, EligibleCandidates and Takes are the table's counts.
	Days               int     `json:"days"`
	EligibleCandidates int     `json:"eligible_candidates"`
	Takes              int     `json:"takes"`
	SumNetBps          float64 `json:"sum_net_bps"`
	// PerEligibleBps is the mean net bps per eligible candidate (skip = 0) and
	// PerTakeBps per take (nil without a take): separate columns, because the
	// denominators differ across cells.
	PerEligibleBps float64  `json:"per_eligible_candidate_bps"`
	PerTakeBps     *float64 `json:"per_take_bps"`
}

// ComparisonResult is one paired comparison (To - From) on the per-eligible-
// candidate column.
type ComparisonResult struct {
	From string `json:"from"`
	To   string `json:"to"`
	stats.Result
}

// Reversion counts the VWAP reversion candidates, reported apart from the Q
// difference (and from every cell): Q does not apply to them and their label
// belongs to Jev's direction, not to the side the rule trades.
type Reversion struct {
	Rows        int `json:"rows"`
	ReachedMeta int `json:"reached_meta"`
	ModelTakes  int `json:"model_takes"`
	ModelSkips  int `json:"model_skips"`
}

// Reference is population (i) on the original OOS: the guard-free rescoring of
// the rows the model was tested on. Reference only: it is not independent, has
// no interval and no verdict, and never enters table 1.
type Reference struct {
	Rows       int `json:"rows"`
	Rescored   int `json:"rescored"`
	Incomplete int `json:"incomplete"`
	Takes      int `json:"takes"`
	// MeanNetBpsPerRow is per rescored row (skip = 0), MeanNetBpsPerTake per take.
	MeanNetBpsPerRow  *float64 `json:"mean_net_bps_per_row"`
	MeanNetBpsPerTake *float64 `json:"mean_net_bps_per_take"`
}

// Result is table 1 and the population (i) reference.
type Result struct {
	// Boundary describes where independence starts; Independence counts every
	// class and reason of the input rows.
	Boundary     string               `json:"boundary"`
	Independence independence.Summary `json:"independence"`
	Days         []string             `json:"days"`
	Cells        []CellResult         `json:"cells"`
	// Comparisons are the four primary one-axis comparisons (with verdicts)
	// followed by the diagonals (reference only, no verdict).
	Comparisons []ComparisonResult `json:"comparisons"`
	Reversion   Reversion          `json:"reversion"`
	Reference   Reference          `json:"reference_original_oos"`
	// PrimaryComparisons is the number of comparisons with a verdict that ran.
	PrimaryComparisons int `json:"primary_comparisons"`
	// PopulationLimit states the rows excluded as unknown (cannot be
	// recomputed), "" when there are none.
	PopulationLimit string `json:"population_limit"`
}
