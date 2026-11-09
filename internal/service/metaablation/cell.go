// Package metaablation is the offline 2x2 comparison of the meta-labeling
// model (M axis) and the entry_quality cut-off (Q axis), run on a COPY of the
// database with a rejected model re-scored from its stored params (issue
// #1046). It is research tooling: it never trades, never writes to the source
// database, never promotes a model and never calls an LLM.
//
// This file is the contract shared by the sub-packages (label layer, replay
// layer, statistics, report): the four cells and the stage order. The stage
// order is the live application order (internal/service/policy/direction.go,
// meta.go, engine.go): eligibility -> Q -> flow guards -> M -> Risk -> fill.
package metaablation

// QualityCut is the entry_quality floor of the Q axis when it is on. It is
// fixed (pre-registered), never searched on the evaluation window.
const QualityCut = "good"

// Cell is one of the four cells of the comparison. MOn: the re-scored model's
// verdict gates the entry (metalabel.Decide); off: the M stage is skipped (the
// candidate is NOT ordered unconditionally - Risk, capital and exits still
// apply). QOn: entry_quality >= QualityCut is required; off: the entry_quality
// cut is not applied (the other guards are).
type Cell struct {
	MOn bool
	QOn bool
}

// Cells lists the four cells in a fixed order: (M off, Q off), (M off, Q on),
// (M on, Q off), (M on, Q on).
func Cells() [4]Cell {
	return [4]Cell{{false, false}, {false, true}, {true, false}, {true, true}}
}

// Name is "m_off_q_off", "m_off_q_on", "m_on_q_off" or "m_on_q_on".
func (c Cell) Name() string {
	m, q := "m_off", "q_off"
	if c.MOn {
		m = "m_on"
	}
	if c.QOn {
		q = "q_on"
	}
	return m + "_" + q
}

// Comparison is a pair of cells that differ in exactly one axis (a primary
// comparison) or in both (a diagonal, reference only). To is compared against
// From as To - From.
type Comparison struct {
	Name     string
	From, To Cell
	// Primary is false for the diagonals, which are reference only.
	Primary bool
}

// Comparisons are the four primary one-axis comparisons followed by the two
// diagonals (reference only, never the basis of a verdict).
func Comparisons() []Comparison {
	moffqoff, moffqon, monqoff, monqon := Cells()[0], Cells()[1], Cells()[2], Cells()[3]
	return []Comparison{
		{"q_off_to_on@m_off", moffqoff, moffqon, true},
		{"q_off_to_on@m_on", monqoff, monqon, true},
		{"m_off_to_on@q_off", moffqoff, monqoff, true},
		{"m_off_to_on@q_on", moffqon, monqon, true},
		{"diagonal_both_off_to_both_on", moffqoff, monqon, false},
		{"diagonal_m_off_q_on_to_m_on_q_off", moffqon, monqoff, false},
	}
}

// Stage is where a row first drops out, numbered in the live application
// order. A row is counted at the FIRST stage that rejects it.
type Stage int

const (
	StageDecision    Stage = iota + 1 // Jev decision -> LONG/SHORT candidate
	StageLabeled                      // labeled (label layer only)
	StageFeatures                     // common features present; unrecoverable rows are "unknown" and excluded from all four cells
	StageEligibility                  // API error, missing data, cohort, spread, thin book, special quote, price limit, short-sell availability, VWAP rule
	StageQuality                      // entry_quality < QualityCut (Q on only)
	StageFlowGuards                   // continuation -> toxic_flow -> liquidity_stressed (common to all cells)
	StageMeta                         // meta-model verdict skip (M on only)
	StageRisk                         // Risk Engine (replay layer only)
	StageFill                         // fill and exit (replay layer only)
)

// String is the stable machine-readable stage name used in reports.
func (s Stage) String() string {
	switch s {
	case StageDecision:
		return "decision"
	case StageLabeled:
		return "labeled"
	case StageFeatures:
		return "features"
	case StageEligibility:
		return "eligibility"
	case StageQuality:
		return "quality"
	case StageFlowGuards:
		return "flow_guards"
	case StageMeta:
		return "meta"
	case StageRisk:
		return "risk"
	case StageFill:
		return "fill"
	default:
		return "unknown"
	}
}
