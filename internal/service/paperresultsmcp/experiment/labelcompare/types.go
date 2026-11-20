// Package labelcompare is the label-based side of experiment_compare (issue
// #1088): a before / after comparison of a tuning experiment that needs no
// closed Paper trade. It compares the trader's decisions (LONG / SHORT / NONE
// counts), their cost-inclusive stored labels (calibration_outcomes,
// label_version 2, net_return_bps: the label that already carries
// costsettings.LabelBarriers' cost), the skips (trade_signals rejections, by
// fixed reason code) and the research labels (#1074: research_label_day_stats)
// of the experiment's before and after windows, with a day-clustered 95%
// interval (Newey-West corrected for the overlap of neighbouring days: newey.go) and the number of days still needed for a verdict.
//
// It is research output only and never an input of trading: it reads existing
// tables through the MCP's read-only connection, writes nothing, and its
// label_verdict never reverts or changes a setting. The trade-based verdict of
// experiment_compare is untouched; this result is returned in its own member.
package labelcompare

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"

// Label verdicts. They are deliberately a separate vocabulary from the trade
// verdict (experiment.Verdict*): the field is label_verdict, not verdict, and
// never replaces it. The first three are the experiment-level rules the trade
// verdict also applies (the confounder / switch-check result of the same call
// is reused, so a confounded experiment is not judged here either).
const (
	VerdictAborted           = "aborted"
	VerdictConfounded        = "confounded"
	VerdictSwitchUnconfirmed = "switch_unconfirmed"
	// VerdictInsufficientDays: a window has fewer than MinDaysFloor JST days
	// with a labeled LONG / SHORT decision. The figures are returned, the
	// judgement is withheld.
	VerdictInsufficientDays = "insufficient_days"
	// VerdictNotComputable: enough days, but the interval of the difference
	// does not exist (every day of both windows has the same value).
	VerdictNotComputable = "not_computable"
	// VerdictImproved / VerdictWorsened: the 95% interval of after minus before
	// of the mean label net bps (per-day means) lies wholly above / below 0.
	VerdictImproved     = "improved"
	VerdictWorsened     = "worsened"
	VerdictInconclusive = "inconclusive"
)

// Error codes of Result.Error: why the label-based side is missing while the
// rest of experiment_compare is returned.
const (
	// ErrorLimitExceeded: a window holds more trader decisions (MaxDecisions) or
	// JST days (MaxWindowDays) than one read may scan inside the 3 s budget.
	ErrorLimitExceeded = "limit_exceeded"
	// ErrorFailed: any other failure of the label-based reads.
	ErrorFailed = "failed"
)

// Reasons DaysNeeded.Reason gives for a missing statistical figure.
const (
	// ReasonVarianceUndefined: a window has fewer than two days, so the spread
	// of the daily means is unknown.
	ReasonVarianceUndefined = "variance_undefined"
	// ReasonDiffNearZero: after minus before is (almost) 0; no number of days
	// makes an interval narrower than 0.
	ReasonDiffNearZero = "diff_near_zero"
	// ReasonBeyondHorizon: more than MaxDaysNeeded days per side would be needed.
	ReasonBeyondHorizon = "beyond_horizon"
)

// Limits and constants of the label-based side.
const (
	// MaxDecisions bounds the trader decisions one window may hold; beyond it
	// the label-based side is not computed (ErrorLimitExceeded).
	MaxDecisions = 200_000
	// MaxWindowDays bounds the length of a window in days.
	MaxWindowDays = 400
	// MaxDaysNeeded bounds the days-per-side search; a larger need is
	// ReasonBeyondHorizon.
	MaxDaysNeeded = 250
	// NearZeroBps: |diff| below it counts as 0 for the days-needed search.
	NearZeroBps = 1e-9
	// TopReasons is how many skip reason codes a window lists.
	TopReasons = 5
	// MinDaysFloor is the JST days each window needs with a labeled LONG /
	// SHORT decision before the label-based side is judged.
	//
	// 8 is the smallest n with n >= 2 * 1.96^2 (about 7.7): the days per
	// window at which the 95% half-width of a difference of two daily means
	// is at most one daily standard deviation, whatever that deviation is
	// (half-width = 1.96 * sigma * sqrt(2/n) <= sigma). The derivation and
	// what was (and was not) measured are in
	// docs/architecture/paper-results-mcp-experiments-labels.md §2.4. It is a
	// floor under the statistical days-needed figure, not a power calculation.
	MinDaysFloor = 8
)

// MinDaysSource says where the min_days of a response came from: the
// derivation of MinDaysFloor above, not a provisional placeholder.
const MinDaysSource = "measured_v1"

// Caution is returned with every answer.
const Caution = "ラベルによる判定は実現損益（取引の純損益）ではない。ラベルは約定ではなく「その時点で入っていたら」の仮想の結果で、" +
	"保有期間の重なるラベルは時間的に重なり互いに独立ではない（区間は日を 1 まとまりとし、前後の日の相関は Newey-West で補正している。補正しても日数が少ないと区間は楽観的になりうる）。" +
	"前後は別の期間で因果の証明にならない。取引ベースの判定（verdict / judged）の代わりにはならず、補助の証拠としてだけ使うこと。" +
	"読み取り専用で、設定・モデル・プロンプトを変えたり戻したりしない"

// Stat is a mean with its day-clustered, Newey-West corrected 95% interval. N counts the
// observations (decisions or snapshots) behind it, Days the JST days; the mean
// is the mean of the per-day means, so every day weighs the same. The mean and
// the interval are null when there is no day (the interval: fewer than two).
type Stat struct {
	N          int      `json:"n"`
	Days       int      `json:"days"`
	MeanNetBps *float64 `json:"mean_net_bps"`
	CILow      *float64 `json:"ci_low"`
	CIHigh     *float64 `json:"ci_high"`
}

// Diff is after minus before of two windows' daily means: Welch's two-sample
// interval on the per-day means (the unit of analysis is the day) with each
// window's variance of the mean Newey-West corrected (diffOf), null when not
// computable.
type Diff struct {
	DiffBps float64  `json:"diff_bps"`
	CILow   *float64 `json:"ci_low"`
	CIHigh  *float64 `json:"ci_high"`
	T       *float64 `json:"t"`
	DF      *float64 `json:"df"`
}

// SideCompare is one series (a side, a horizon) in both windows and its difference.
type SideCompare struct {
	Before Stat  `json:"before"`
	After  Stat  `json:"after"`
	Diff   *Diff `json:"diff"`
}

// Decisions counts a window's trader decisions by direction. Unknown is the
// decisions with no direction recorded.
type Decisions struct {
	Long    int `json:"long"`
	Short   int `json:"short"`
	None    int `json:"none"`
	Unknown int `json:"unknown"`
	Total   int `json:"total"`
}

// Skips are a window's skipped (rejected) trade signals: the Policy Engine's
// own skips and the Risk Engine's rejections, by fixed reason code (never free
// text). Null in a window whose rejections were not read.
type Skips struct {
	SkipCount       int `json:"skip_count"`
	RiskPassedCount int `json:"risk_passed_count"`
	// SkipRate is skip_count / (skip_count + risk_passed_count); null with no signal.
	SkipRate   *float64             `json:"skip_rate"`
	TopReasons []query.RejectBucket `json:"top_reasons"`
}

// Window is one side's period.
type Window struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Days are the JST days of the window with a labeled LONG / SHORT decision.
	Days      int       `json:"days"`
	Decisions Decisions `json:"decisions"`
	Skips     *Skips    `json:"skips"`
}

// DecisionLabels compares the stored cost-inclusive label of the trader's
// LONG / SHORT decisions: Long and Short by the decision's own direction, All
// both together (the one the label verdict rests on).
type DecisionLabels struct {
	// HorizonMinutes is the stored label's horizon (calibration_outcomes.horizon_minutes)
	// compared; labels of another horizon are left out (IgnoredRows).
	HorizonMinutes int         `json:"horizon_minutes"`
	IgnoredRows    int         `json:"ignored_rows"`
	Long           SideCompare `json:"long"`
	Short          SideCompare `json:"short"`
	All            SideCompare `json:"all"`
}

// ResearchHorizon is the research labels (#1074) of one horizon over every
// recorded snapshot (not only decisions), per side, in whole JST days the
// recorder completed.
type ResearchHorizon struct {
	Horizon string      `json:"horizon"`
	Long    SideCompare `json:"long"`
	Short   SideCompare `json:"short"`
}

// DaysNeeded is the number of days still needed for a verdict.
type DaysNeeded struct {
	// ObservedPerSide is the smaller of the two windows' days with a labeled decision.
	ObservedPerSide int `json:"observed_per_side"`
	// MinDays is MinDaysFloor; MinDaysSource says where it comes from.
	MinDays       int    `json:"min_days"`
	MinDaysSource string `json:"min_days_source"`
	// DaysToMin is max(0, MinDays - ObservedPerSide).
	DaysToMin int `json:"days_to_min"`
	// NeededPerSide is the days per window at which the 95% half-width of the
	// difference falls below |diff| at the current variance (the t quantile at
	// 2*days-2 degrees of freedom; at least MinDays). Null with Reason.
	NeededPerSide *int `json:"needed_per_side"`
	// RemainingDays is max(DaysToMin, NeededPerSide - ObservedPerSide); null with Reason.
	RemainingDays *int   `json:"remaining_days"`
	Reason        string `json:"reason,omitempty"`
}

// Result is experiment_compare's label_based member. When Error is set the
// figures are absent (the rest of the response is unaffected).
type Result struct {
	// Error is an Error* code when the label-based side could not be computed.
	Error   string `json:"error,omitempty"`
	Caution string `json:"caution"`
	// IntervalMethod (IntervalMethod) and IntervalLagDays (NeweyWestLagDays) say
	// how every interval below was made.
	IntervalMethod  string `json:"interval_method"`
	IntervalLagDays int    `json:"interval_lag_days"`

	LabelVerdict string `json:"label_verdict"`
	LabelJudged  bool   `json:"label_judged"`

	Before         Window            `json:"before"`
	After          Window            `json:"after"`
	DecisionLabels *DecisionLabels   `json:"decision_labels"`
	ResearchLabels []ResearchHorizon `json:"research_labels"`
	DaysNeeded     *DaysNeeded       `json:"days_needed"`
}
