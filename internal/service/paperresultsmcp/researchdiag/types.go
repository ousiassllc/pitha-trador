// Package researchdiag is the read-only analysis behind the research_horizon_summary
// tool of the Paper results MCP (issue #1074,
// docs/api/endpoints/paper-results-mcp-research-labels.md §3.34): what holding
// to 30 minutes, 60 minutes and the close returned before and after the label's
// assumed cost, against the real spread, from research_horizon_labels. It reads
// only that table, research_label_days, market_snapshots (counts) and
// jev_decisions (to pick the decisions of a scope); it writes nothing, and its
// numbers are descriptive research output, never an input of trading.
package researchdiag

// Scopes of the tool: which rows the statistics are over.
const (
	// ScopeSnapshots is every labeled snapshot, both sides reported.
	ScopeSnapshots = "snapshots"
	// ScopeTrader is the snapshots a trader decision (LONG/SHORT) was made on,
	// at the decision's own direction.
	ScopeTrader = "trader"
	// ScopeScout is the same for scout decisions.
	ScopeScout = "scout"
)

// Statistics constants (descriptive, not adoption thresholds).
const (
	// Alpha is the two-sided level of the bootstrap interval and of the
	// detectable minimum; Power the power of the detectable minimum.
	Alpha = 0.05
	Power = 0.8
	// Resamples is the number of day-clustered bootstrap draws and Seed their
	// fixed stream, so the same data gives the same interval.
	Resamples = 2000
	Seed      = 1074
	// MedianSampleCap bounds the rows read per horizon for the medians: a
	// horizon with more rows reads the k% of them whose sample_bucket is below
	// k, k = 100 * cap / rows (at least 1).
	MedianSampleCap = 100_000
)

// Notice is returned with every answer.
const Notice = "研究専用の記録の集計であり、取引の入力ではない。Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・メタモデルの学習はこの値を読まない。" +
	"しきい値の判定・採用の推奨・優劣の結論は含まない（検出できる最小差も、固定の α=0.05（両側）・検出力 0.8 での記述であり、採用の基準ではない）"

// Interval is a percentile interval of the mean net return.
type Interval struct {
	Lower float64 `json:"lower_bps"`
	Upper float64 `json:"upper_bps"`
}

// SideStats are the statistics of one side at one horizon. Gross is before the
// label's assumed cost, Net after it (net = gross - cost, per row).
type SideStats struct {
	Rows int `json:"rows"`
	Days int `json:"days"`
	// MeanGrossBps / MeanNetBps are over all rows. MedianGrossBps /
	// MedianNetBps (and the medians of cost_vs_spread) are exact while
	// MedianSamplePercent is 100, and the medians of a k% sample of the rows
	// (by a hash of the snapshot id) below that.
	MeanGrossBps        float64 `json:"mean_gross_bps"`
	MeanNetBps          float64 `json:"mean_net_bps"`
	MedianGrossBps      float64 `json:"median_gross_bps"`
	MedianNetBps        float64 `json:"median_net_bps"`
	MedianSamplePercent int     `json:"median_sample_percent"`
	// WinRate is the share of rows with net > 0.
	WinRate float64 `json:"win_rate"`
	// MeanNetCI is the day-clustered bootstrap interval of MeanNetBps at level
	// 1 - Alpha; SEBps its bootstrap standard deviation and
	// DetectableMinimumBps = (z(1 - Alpha/2) + z(Power)) * SE, the smallest
	// true mean net the days observed could have shown at that level and
	// power. All three are null with fewer than two days.
	MeanNetCI            *Interval `json:"mean_net_ci"`
	SEBps                *float64  `json:"mean_net_se_bps"`
	DetectableMinimumBps *float64  `json:"detectable_minimum_bps"`
}

// Distribution is a mean and a median.
type Distribution struct {
	MeanBps   float64 `json:"mean_bps"`
	MedianBps float64 `json:"median_bps"`
}

// SideCost is the assumed cost of a side against the entry bar's real spread.
type SideCost struct {
	Rows int `json:"rows"`
	// AssumedCost is the label's cost per row (long_cost_bps / short_cost_bps).
	AssumedCost Distribution `json:"assumed_cost"`
	// RowsWithSpread are the rows whose entry bar has a spread_bps; the
	// following fields are over them (null when there are none).
	RowsWithSpread int           `json:"rows_with_spread"`
	Spread         *Distribution `json:"entry_spread"`
	// NetAfterSpreadBps is the mean of gross - spread_bps / 2.
	NetAfterSpreadBps *float64 `json:"net_after_spread_bps"`
	// ShareSpreadAboveCost is the share of RowsWithSpread with spread_bps >
	// the assumed cost.
	ShareSpreadAboveCost *float64 `json:"share_spread_above_cost"`
}

// CostVsSpread is SideCost for each side.
type CostVsSpread struct {
	Long  *SideCost `json:"long"`
	Short *SideCost `json:"short"`
}

// HorizonStats are one horizon's statistics. Long / Short are null when the
// scope has no row of that side (a decision scope shows the decision's own
// direction only).
type HorizonStats struct {
	Horizon      string       `json:"horizon"`
	Long         *SideStats   `json:"long"`
	Short        *SideStats   `json:"short"`
	CostVsSpread CostVsSpread `json:"cost_vs_spread"`
}

// DayCoverage is one JST day's snapshots against its labeled rows.
type DayCoverage struct {
	Day       string `json:"day"`
	Snapshots int    `json:"snapshots"`
	// Labeled counts the day's research rows per horizon.
	Labeled map[string]int `json:"labeled"`
	// Recorded is whether the recorder marked the day done.
	Recorded bool `json:"recorded"`
}

// Coverage shows how much of the period is labeled, so a missing day is visible.
type Coverage struct {
	// Unit is what InPeriod and Labeled count: snapshots (scope snapshots) or
	// decisions (a decision scope: LONG/SHORT decisions, of the filtered direction).
	Unit     string         `json:"unit"`
	InPeriod int            `json:"in_period"`
	Labeled  map[string]int `json:"labeled"`
	// SnapshotDays lists the period's JST days with snapshots or research rows
	// (always snapshots, whatever the scope). UnrecordedDays counts the days
	// among them before today that the recorder has not completed.
	SnapshotDays   []DayCoverage `json:"snapshot_days"`
	UnrecordedDays int           `json:"unrecorded_days"`
}

// Definitions say what every number is.
type Definitions struct {
	Entry     string            `json:"entry"`
	Exit      map[string]string `json:"exit"`
	Cost      string            `json:"cost"`
	Net       string            `json:"net"`
	Spread    string            `json:"spread"`
	Scope     map[string]string `json:"scope"`
	Intervals string            `json:"intervals"`
}

// Data is research_horizon_summary's data.
type Data struct {
	Scope string `json:"scope"`
	// Direction is the direction filter of a decision scope (null: none).
	Direction *string `json:"direction"`
	// LabelVersion is research_horizon_labels.label_version of the rows read.
	LabelVersion int `json:"label_version"`
	// RecordedThrough is the latest JST day the recorder completed (null: none yet).
	RecordedThrough *string        `json:"recorded_through"`
	Horizons        []HorizonStats `json:"horizons"`
	Coverage        Coverage       `json:"coverage"`
	Definitions     Definitions    `json:"definitions"`
	Notice          string         `json:"notice"`
}
