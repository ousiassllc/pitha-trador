package replaylayer

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation"
)

// Experiment labels, carried by every comparison so a reference result is
// never mistaken for the continuous one.
const (
	ExperimentContinuous = "continuous"
	// ExperimentDailyReset is the reference experiment in which every day is
	// replayed independently from the initial state. Its daily series are not a
	// continuous equity curve and have no drawdown.
	ExperimentDailyReset = "reference_daily_independent_reset"
)

// Streams of a daily P&L series: Total is the whole portfolio, Momentum the
// trades opened on Jev's momentum direction and Reversion the VWAP reversion
// trades (which entry_quality never touches).
const (
	StreamTotal     = "total"
	StreamMomentum  = "momentum"
	StreamReversion = "reversion"
)

// Units of a difference series.
const (
	UnitJPY          = "jpy"
	UnitCapitalRatio = "capital_ratio" // yen / the segment's initial capital
)

// Window is the evaluation window [From, To).
type Window struct{ From, To time.Time }

// UnavailablePeriod is a period the replay layer could not run, with why. The
// label layer is not affected by it.
type UnavailablePeriod struct {
	From, To time.Time
	Reason   string
}

// SegmentSpec is one common continuous segment: the same for the four cells.
type SegmentSpec struct {
	Index    int
	From, To time.Time
	// Days are the JST trading days with market data in the segment ("2006-01-02"),
	// the days every cell's daily series is built on.
	Days []string
	// InitialCapitalJPY is the pre-fixed common initial condition of the
	// segment (no open position, no loss streak, no cooldown).
	InitialCapitalJPY float64
}

// DailyPoint is one day of a cell's continuous path in a segment.
type DailyPoint struct {
	Day string
	// PnLJPY is the P&L realized by the trades closed that day;
	// PnLRatio = PnLJPY / the segment's initial capital.
	PnLJPY   float64
	PnLRatio float64
	// MomentumPnLJPY and ReversionPnLJPY split PnLJPY by the kind of entry.
	MomentumPnLJPY  float64
	ReversionPnLJPY float64
	Trades          int
	// EquityJPY is the end-of-day equity: initial capital plus the cumulative
	// P&L of this segment.
	EquityJPY float64
}

// Drawdown describes the maximum drawdown on one segment's daily equity curve.
// It is a description only: with few days its difference between cells is not
// reliable.
type Drawdown struct {
	MaxJPY float64
	// MaxPct is MaxJPY as a percentage of the peak equity it fell from.
	MaxPct float64
	// PeakDay is "start" when the peak is the initial capital; TroughDay is ""
	// when the curve never fell below a previous peak.
	PeakDay, TroughDay string
	Description        string
}

// StageCount is one stage of the live application order: how many decisions
// reached it and how many dropped there (first rejecting stage).
type StageCount struct {
	Stage   string
	Reached int
	Dropped int
}

// Counts are one cell's counts over the decisions the replay evaluated.
// They are per cell and path dependent: the entries of a cell decide which
// later decisions arrive while a position is open.
type Counts struct {
	// DecisionsInWindow are the given decisions timestamped inside the segment
	// (the same for every cell); Evaluated the fresh ones evaluated for an
	// entry while the instrument had no open position; NotEvaluated the rest
	// (superseded before a bar, or arrived with a position open).
	DecisionsInWindow int
	Evaluated         int
	NotEvaluated      int
	// Eligible passed the decision and eligibility stages (reached Q); Takes
	// passed the M stage (reached Risk; with M off everything that passed the
	// flow guards); Entries opened a position; Trades closed ones.
	Eligible int
	Takes    int
	Entries  int
	Trades   int
	// NetPnLJPY sums the closed trades' P&L; NetBpsSum their net return in bps.
	NetPnLJPY float64
	NetBpsSum float64
	// MeanNetBpsPerEntry divides NetBpsSum by Trades ("per take"); the
	// per-eligible figures divide by Eligible, a skip counting 0 ("per eligible
	// candidate"). The two have different denominators and are kept apart.
	MeanNetBpsPerEntry float64
	NetBpsPerEligible  float64
	PnLJPYPerEligible  float64
}

// TradeRecord is one closed trade, aggregated fields only.
type TradeRecord struct {
	Day          string // JST day of the exit
	InstrumentID int64
	DecisionID   int64
	Direction    string
	Reversion    bool
	Quantity     int64
	NetReturnBps float64
	PnLJPY       float64
	ExitReason   string
}

// CellSegment is one cell's results on one common segment.
type CellSegment struct {
	Segment  int
	Daily    []DailyPoint
	Drawdown Drawdown
	Counts   Counts
	// Reversion is Counts restricted to the VWAP reversion candidates, which Q
	// does not affect; the momentum figures are Counts minus Reversion.
	Reversion Counts
	// Stages and ReversionStages count the decisions at the first stage that
	// rejected them, in the live order; ReversionStages is the reversion subset.
	Stages          []StageCount
	ReversionStages []StageCount
	// DropReasons counts the first rejecting reason (stage:reason).
	DropReasons map[string]int
	Trades      []TradeRecord
}

// CellConfig is what one cell changes: nothing but these three values.
type CellConfig struct {
	Entry           string // "meta" (M on) or "no_meta_filter" (M off)
	MinEntryQuality string // metaablation.QualityCut (Q on) or the lowest quality (Q off)
	ModelApplied    bool
}

// CellResult is one cell's results; there is deliberately no series or curve
// spanning segments.
type CellResult struct {
	Cell     metaablation.Cell
	Name     string
	Config   CellConfig
	Segments []CellSegment
	// Totals sum the counts over the segments (counts and yen only).
	Totals          Counts
	ReversionTotals Counts
}

// DailyDiff is one day of an aligned difference series: the cell A and cell B
// values and Diff = B - A.
type DailyDiff struct {
	Day  string
	A, B float64
	Diff float64
}

// DiffSeries is the aligned daily difference of one stream in one unit.
type DiffSeries struct {
	Stream string
	Unit   string
	Days   []DailyDiff
	// Mean is the plain mean of Diff over Days; the interval comes from the
	// statistics package (bootstrap over days).
	Mean float64
}

// ComparisonSegment is a comparison's difference series on one segment.
type ComparisonSegment struct {
	Segment int
	Series  []DiffSeries
}

// ComparisonResult is one comparison (B = To, A = From). A diagonal is
// Primary=false: reference only, never the basis of a verdict.
type ComparisonResult struct {
	Experiment string
	Name       string
	From, To   metaablation.Cell
	Primary    bool
	// Axis is "q", "m" or "both" (diagonal).
	Axis string
	// BasisStream is the stream a verdict is based on: momentum for the Q axis
	// (reversion candidates are not affected by Q, and are shown separately),
	// total otherwise.
	BasisStream string
	Segments    []ComparisonSegment
}

// ReferenceDay is one independently replayed day (no equity, no drawdown).
type ReferenceDay struct {
	Segment         int
	Day             string
	PnLJPY          float64
	PnLRatio        float64
	MomentumPnLJPY  float64
	ReversionPnLJPY float64
}

// ReferenceCell is one cell's independent days.
type ReferenceCell struct {
	Cell metaablation.Cell
	Name string
	Days []ReferenceDay
}

// Reference is the daily-independent-reset experiment: every day starts from
// the initial state. It is NOT the continuous result, has no equity curve or
// drawdown and is not a basis of a verdict.
type Reference struct {
	Label       string
	Cells       []ReferenceCell
	Comparisons []ComparisonResult
}

// Result is the replay layer's output.
type Result struct {
	Window Window
	Config ConfigRecord
	// Population says what table 2 counts, so it is never mixed with the label
	// layer's or the original model evaluation's populations.
	Population string
	// Unavailable lists the periods the replay layer did not run, with reasons.
	Unavailable []UnavailablePeriod
	Segments    []SegmentSpec
	Cells       []CellResult
	Comparisons []ComparisonResult
	// ReferenceDailyReset is nil unless Input.ReferenceDailyReset was set.
	ReferenceDailyReset *Reference
}

// PopulationDescription is Result.Population.
const PopulationDescription = "replay layer (table 2): candidates that passed the cell's Q and M and the Risk Engine and " +
	"capital constraints and were actually entered in a continuous replay; P&L in yen and as a ratio of the initial capital " +
	"from replayed fills and exits. Not comparable with the label layer (net_return_bps proxy, no Risk/capital) or the " +
	"original model evaluation (before guards)."
