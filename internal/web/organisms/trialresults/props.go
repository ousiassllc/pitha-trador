package trialresults

import (
	"fmt"
	"strconv"
)

// PeriodLink is one choice of the period shown (a plain link, `?days=`).
type PeriodLink struct {
	Label   string
	Href    string
	Current bool
}

// Props is Panel's input. Error replaces the figures when they could not be
// read; Empty is true when the period holds no trial record at all.
type Props struct {
	Periods     []PeriodLink
	Error       string
	Empty       bool
	CohortSize  int
	Arms        []Arm
	SkipReasons []SkipRow
	Cost        []CostRow
	Proxy       []ProxyRow
}

// Arm is one arm's calls and missing rate over the period. MissingRate is nil
// when the arm has no call (a skip-only arm), never 0.
type Arm struct {
	ID          string
	Calls       int
	Missing     int
	MissingRate *float64
	Skipped     int
}

// SkipRow is the skips of one arm for one reason (only non-zero counts).
type SkipRow struct {
	ArmID  string
	Reason string
	Count  int
}

// SourceCost is the calls of one known cost source and their summed cost (USD).
type SourceCost struct {
	Calls int
	USD   float64
}

// CostRow is the cost of one arm (or the total) by source. Unknown is only a
// call count: the real cost of those calls is not known and is never a sum.
type CostRow struct {
	Label        string
	Total        bool
	Reported     SourceCost
	Estimate     SourceCost
	UnknownCalls int
}

// ProxyRow is one arm's no-direction decisions and the counterfactual labels
// they carry (proxy: not a realized outcome).
type ProxyRow struct {
	ArmID       string
	NoneAnswers int
	NoAnswer    int
	Horizons    []ProxyHorizon
}

// ProxyHorizon is the counterfactual labels of one horizon.
type ProxyHorizon struct {
	Minutes   int
	Labelled  int
	Coverage  *float64
	TP        int
	SL        int
	Timeout   int
	AvgNetBps *float64
}

const noValue = "—"

func pct(v *float64) string {
	if v == nil {
		return noValue
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}

func bps(v *float64) string {
	if v == nil {
		return noValue
	}
	return fmt.Sprintf("%+.1f bps", *v)
}

func itoa(n int) string { return strconv.Itoa(n) }

// missingText is "12.5%（1 / 8）", or "—" for an arm with no call.
func missingText(a Arm) string {
	if a.MissingRate == nil {
		return noValue
	}
	return fmt.Sprintf("%s（%d / %d）", pct(a.MissingRate), a.Missing, a.Calls)
}

// sourceText is a known source's cell: "$0.0123（5 件）", or "—" without a call.
func sourceText(s SourceCost) string {
	if s.Calls == 0 {
		return noValue
	}
	return fmt.Sprintf("$%.4f（%d 件）", s.USD, s.Calls)
}

// unknownText is the unknown cell: "不明（3 件）". It never shows an amount, so
// it can not read as 0.
func unknownText(calls int) string {
	if calls == 0 {
		return "なし"
	}
	return fmt.Sprintf("不明（%d 件）", calls)
}

func coverageText(h ProxyHorizon) string {
	return fmt.Sprintf("%s（%d 件）", pct(h.Coverage), h.Labelled)
}
