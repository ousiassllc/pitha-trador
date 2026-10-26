package metalabeldiag

import (
	"math"
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The values of ReadinessSplit.OOSStartReason: what decides where the OOS
// window opens (issue #913).
const (
	// OOSStartCutoffFloor: the rows from llm_cutoff on do not hold the OOS
	// minimums (rows and days), so the window opens at llm_cutoff itself.
	OOSStartCutoffFloor = "cutoff_floor"
	// OOSStartMaxWindow: the same, but llm_cutoff is older than the longest
	// window (OOSMaxWindow before now), so the window opens at that limit.
	OOSStartMaxWindow = "max_window"
	// OOSStartMinRows: the window opens where it holds exactly the minimum rows.
	OOSStartMinRows = "min_rows"
	// OOSStartMinDays: the window opens at the first row of the newest
	// minimum-days trading days (more rows than the minimum fit in it).
	OOSStartMinDays = "min_days"
)

// RateTradingDays is how many of the newest completed trading days
// Shortfall.RowsPerTradingDay averages the usable rows over.
const RateTradingDays = 5

// Shortfall is how many more usable rows the retraining needs before it can
// learn and judge a model, and how long that takes at the recent rate. It only
// reads: the adoption bar and the walk-forward configuration are the
// retraining's own (training.DefaultAcceptanceConfig / DefaultWalkForwardConfig).
type Shortfall struct {
	// OOSWindowRows is the usable rows decided on or after the window floor
	// (the OOS window can only reach back that far); OOSRowsMissing is how many
	// the bar's minimum lacks.
	OOSWindowRows  int `json:"oos_window_rows"`
	OOSRowsMissing int `json:"oos_rows_missing"`
	// PurgedRows are the rows dropped at the boundary of the OOS window (their
	// label window reaches into it); TrainRowsMissing is how many rows the fit
	// and calibration minimum lacks after that purge.
	PurgedRows       int `json:"purged_rows"`
	TrainRowsMissing int `json:"train_rows_missing"`
	// AdditionalRowsNeeded is the new usable rows that make both sides ready:
	// max(OOS minimum − window rows, OOS minimum + fit/calibration minimum +
	// purged − all usable rows, 0). Days are not counted here (split.oos).
	AdditionalRowsNeeded int `json:"additional_rows_needed"`
	// RowsPerTradingDay is the usable rows a day over the RateTradingDays newest
	// completed trading days; EstimatedTradingDays is AdditionalRowsNeeded at
	// that rate, rounded up (0 when nothing is needed, null at a zero rate).
	RowsPerTradingDay    float64 `json:"rows_per_trading_day"`
	RateTradingDays      int     `json:"rate_trading_days"`
	EstimatedTradingDays *int    `json:"estimated_trading_days"`
	// OOSWindowTradingDays are the trading days between the window floor and
	// as-of; OOSMinRowsPerTradingDay is the daily rate that fills the OOS
	// minimum within them (null with no trading day) and RateReachesOOSBar says
	// whether RowsPerTradingDay is at least that.
	OOSWindowTradingDays    int   `json:"oos_window_trading_days"`
	OOSMinRowsPerTradingDay *int  `json:"oos_min_rows_per_trading_day"`
	RateReachesOOSBar       *bool `json:"rate_reaches_oos_bar"`
}

// oosReach is where the OOS window can open and what decides it.
type oosReach struct {
	start       time.Time // where the window opens
	floor       time.Time
	floorSource string // OOSStartCutoffFloor or OOSStartMaxWindow
	reason      string
	reachable   []training.Row // eligible rows decided on or after the floor, oldest first
}

// reachOOS reads the rule training.SplitWalkForward opens its OOS window by
// (WalkForwardConfig.oosStart, unexported) over the rows it would use as of
// asOf, and says which part of it decided. The caller checks the start it gets
// against the split's own OOSStart (metalabel_readiness_window_test.go does so
// on many shapes), so a change of that rule shows up as a failure, not as a
// wrong reason.
func reachOOS(sorted []training.Row, asOf, cutoff time.Time, walk training.WalkForwardConfig) oosReach {
	floor, source := asOf.Add(-walk.OOSMaxWindow), OOSStartMaxWindow
	if cutoff.After(floor) {
		floor, source = cutoff, OOSStartCutoffFloor
	}
	r := oosReach{start: floor, floor: floor, floorSource: source, reason: source}
	if floor.After(asOf) {
		r.start = asOf
		return r
	}
	first := sort.Search(len(sorted), func(i int) bool { return !sorted[i].DecidedAt.Before(floor) })
	r.reachable = sorted[first:]
	byRows, byDays := len(r.reachable)-walk.OOSMinRows, firstRowOfNewestDays(r.reachable, walk.OOSMinDays)
	i := min(byRows, byDays)
	if i <= 0 { // the window reaches the floor
		return r
	}
	r.start = r.reachable[i].DecidedAt
	r.reason = OOSStartMinDays
	if byRows <= byDays {
		r.reason = OOSStartMinRows
	}
	return r
}

// firstRowOfNewestDays is the index of the first row of the days-th newest JST
// day of the time-ordered rows (0 when they span fewer days), as training's.
func firstRowOfNewestDays(rows []training.Row, days int) int {
	seen, prev := 0, ""
	for j := len(rows) - 1; j >= 0; j-- {
		if d := rows[j].DecidedAt.In(marketcalendar.JST).Format(time.DateOnly); d != prev {
			seen, prev = seen+1, d
			if seen > days {
				return j + 1
			}
		}
	}
	return 0
}

// eligibleRows are the rows SplitWalkForward uses as of asOf (decided before
// it, label complete by it), oldest first.
func eligibleRows(rows []training.Row, asOf time.Time) []training.Row {
	out := make([]training.Row, 0, len(rows))
	for _, r := range rows {
		if r.DecidedAt.Before(asOf) && !r.LabelEnd.After(asOf) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DecidedAt.Before(out[j].DecidedAt) })
	return out
}

// shortfall counts what is missing against the retraining's minimums, given
// the window start the split actually used.
func shortfall(sorted []training.Row, reach oosReach, oosStart, asOf time.Time, walk training.WalkForwardConfig) Shortfall {
	var pre, purged int
	for _, r := range sorted {
		if !r.DecidedAt.Before(oosStart) {
			break
		}
		pre++
		if r.LabelEnd.Add(walk.Embargo).After(oosStart) {
			purged++
		}
	}
	trainMin := walk.MinFit + walk.MinCalibration
	s := Shortfall{
		OOSWindowRows: len(reach.reachable), PurgedRows: purged, RateTradingDays: RateTradingDays,
		OOSRowsMissing:   max(0, walk.OOSMinRows-len(reach.reachable)),
		TrainRowsMissing: max(0, trainMin-(pre-purged)),
	}
	s.AdditionalRowsNeeded = max(s.OOSRowsMissing, walk.OOSMinRows+trainMin+purged-len(sorted), 0)

	s.RowsPerTradingDay = rowsPerTradingDay(sorted, asOf, RateTradingDays)
	if s.AdditionalRowsNeeded == 0 {
		s.EstimatedTradingDays = new(0)
	} else if s.RowsPerTradingDay > 0 {
		s.EstimatedTradingDays = new(int(math.Ceil(float64(s.AdditionalRowsNeeded) / s.RowsPerTradingDay)))
	}
	for d := reach.floor.In(marketcalendar.JST); !d.After(asOf); d = d.AddDate(0, 0, 1) {
		if marketcalendar.TSE.IsTradingDay(d) {
			s.OOSWindowTradingDays++
		}
	}
	if s.OOSWindowTradingDays > 0 {
		perDay := (walk.OOSMinRows + s.OOSWindowTradingDays - 1) / s.OOSWindowTradingDays
		s.OOSMinRowsPerTradingDay = &perDay
		s.RateReachesOOSBar = new(s.RowsPerTradingDay >= float64(perDay))
	}
	return s
}

// rowsPerTradingDay is the rows decided on the n newest trading days before
// asOf's JST day, divided by n (a day without rows counts).
func rowsPerTradingDay(sorted []training.Row, asOf time.Time, n int) float64 {
	days := map[string]struct{}{}
	for d := asOf.In(query.JST).AddDate(0, 0, -1); len(days) < n && asOf.Sub(d) < 60*24*time.Hour; d = d.AddDate(0, 0, -1) {
		if marketcalendar.TSE.IsTradingDay(d) {
			days[d.Format(time.DateOnly)] = struct{}{}
		}
	}
	if len(days) == 0 {
		return 0
	}
	rows := 0
	for _, r := range sorted {
		if _, ok := days[r.DecidedAt.In(query.JST).Format(time.DateOnly)]; ok {
			rows++
		}
	}
	return float64(rows) / float64(len(days))
}
