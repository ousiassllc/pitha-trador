package dailyguardreplay

import (
	"sort"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
)

// TradingDayZone bounds the trading day: TSE trades in JST.
var TradingDayZone = time.FixedZone("JST", 9*60*60)

// DayLayout formats a trading day (YYYY-MM-DD, JST).
const DayLayout = "2006-01-02"

// Limitations are the caveats every report carries.
var Limitations = []string{
	"確定損益（realized P&L）だけで再現しています。含み損益は記録から復元できないため、エンジンが含み損で発動する停止や、含み益だけで到達した利益確保の最高値は反映されません。",
	"持ち越しポジション（建てた日と決済した日が異なるもの）は、建てた日の取引回数に数え、決済した日の確定損益に入れます。-from より前に建てたものは、その建玉日のうち範囲に触れない取引を読み込まないため、見送り判定が不完全です（多くは採用扱い）。-to より後に決済した・未決済のものは、建てた日の取引回数だけに数え、確定損益には入りません。",
	"エンジンの判定は Check の時点（1分ごと）の含み損益込みの値で行うのに対し、再現はイベント（建玉・決済）ごとに確定損益を評価するため、停止が発動する時刻・その直前に通った新規の数は近似です。",
	"事後の比較であり、将来の成績の予測ではありません。結果の評価と Live の既定有効化の基準は決定待ちです（issue #782）。",
}

// Trade is one position as recorded. A zero ClosedAt means it was still open
// when read: it counts as an opened trade
// and has no close. PnLMissing marks a NULL realized P&L of a closed
// position, counted as 0 yen like the engine does.
type Trade struct {
	ID         int64
	Symbol     string
	OpenedAt   time.Time
	ClosedAt   time.Time
	PnLJPY     float64
	PnLMissing bool
}

// DayResult is one JST trading day. Trade counts are by the day the position
// was opened (the engine's trade count); P&L figures are by the day it was
// closed (the engine's realized P&L), so Without == With + Skipped.
type DayResult struct {
	Day string
	// TradesAll/TradesKept/TradesSkipped: positions opened that day, the ones
	// the rules would have let through, and the ones they would have refused.
	TradesAll, TradesKept, TradesSkipped int
	// PnLWithoutJPY: every trade closed that day; PnLWithJPY: only kept ones;
	// PnLSkippedJPY: the refused ones (hindsight: what the halt gave up or avoided).
	PnLWithoutJPY, PnLWithJPY, PnLSkippedJPY float64
	// MaxDrawdownWithoutJPY/MaxDrawdownWithJPY: the largest fall of the
	// realized P&L from its intraday peak (the day starts at 0), >= 0.
	MaxDrawdownWithoutJPY, MaxDrawdownWithJPY float64
	// HaltReason is the domain.DailyHaltReason* that stopped entries, "" if none;
	// HaltedAt when (the event that reached it).
	HaltReason string
	HaltedAt   time.Time
	// MissingPnL: positions closed that day whose realized P&L was NULL
	// (counted as 0 yen).
	MissingPnL int
}

// Report is a replay's result. Days are in date order.
type Report struct {
	Limits      dailyguardsettings.Limits
	Days        []DayResult
	Total       DayResult // sums; Day is empty, HaltReason unused, drawdowns are the worst day's
	HaltedDays  int
	MissingPnL  int // closes whose realized P&L was NULL (counted as 0); the sum of the days'
	Limitations []string
}

type event struct {
	at    time.Time
	open  bool
	trade int // index into trades
}

type dayState struct {
	DayResult
	guard                 *Guard
	pnl                   float64 // kept, realized so far
	peak, allPnl, allPeak float64
	keptOpened            int
}

// Replay runs the rules over trades. For each JST day events happen in time
// order (a close before an open at the same instant, then by ID): an open is
// refused when the day is already halted or the kept opened count has reached
// the limit; after each event the Guard re-evaluates the kept realized P&L.
// A refused trade's close is ignored. Only days with activity are reported
// (which includes days outside the range the caller asked for: see
// Report.Restrict). A halt latches for the day and the next day starts fresh.
func Replay(trades []Trade, limits dailyguardsettings.Limits) Report {
	events := make([]event, 0, len(trades)*2)
	for i, t := range trades {
		events = append(events, event{t.OpenedAt, true, i})
		if !t.ClosedAt.IsZero() {
			events = append(events, event{t.ClosedAt, false, i})
		}
	}
	sort.SliceStable(events, func(a, b int) bool {
		ea, eb := events[a], events[b]
		if !ea.at.Equal(eb.at) {
			return ea.at.Before(eb.at)
		}
		if ea.open != eb.open {
			return !ea.open
		}
		return trades[ea.trade].ID < trades[eb.trade].ID
	})

	report := Report{Limits: limits, Limitations: Limitations}
	days := map[string]*dayState{}
	state := func(at time.Time) *dayState {
		day := at.In(TradingDayZone).Format(DayLayout)
		s := days[day]
		if s == nil {
			s = &dayState{DayResult: DayResult{Day: day}, guard: NewGuard(limits)}
			days[day] = s
		}
		return s
	}
	kept := make([]bool, len(trades))
	for _, e := range events {
		s := state(e.at)
		if e.open {
			s.TradesAll++
			if s.guard.Halted() != "" || (limits.MaxTradesEnabled() && s.keptOpened >= limits.MaxTrades) {
				s.TradesSkipped++
				continue
			}
			kept[e.trade] = true
			s.TradesKept++
			s.keptOpened++
		} else {
			if trades[e.trade].PnLMissing {
				s.MissingPnL++
			}
			s.close(trades[e.trade].PnLJPY, kept[e.trade])
		}
		if s.guard.Halted() == "" && s.guard.Update(s.pnl, s.keptOpened) != "" {
			s.HaltReason, s.HaltedAt = s.guard.Halted(), e.at
		}
	}

	for _, s := range days {
		report.Days = append(report.Days, s.DayResult)
	}
	sort.Slice(report.Days, func(a, b int) bool { return report.Days[a].Day < report.Days[b].Day })
	report.sum()
	return report
}

// close books a position closing that day: every close counts without the
// guard, only a kept one counts with it.
func (s *dayState) close(pnl float64, kept bool) {
	s.PnLWithoutJPY += pnl
	s.allPnl += pnl
	s.allPeak = max(s.allPeak, s.allPnl)
	s.MaxDrawdownWithoutJPY = max(s.MaxDrawdownWithoutJPY, s.allPeak-s.allPnl)
	if !kept {
		s.PnLSkippedJPY += pnl
		return
	}
	s.pnl += pnl
	s.PnLWithJPY += pnl
	s.peak = max(s.peak, s.pnl)
	s.MaxDrawdownWithJPY = max(s.MaxDrawdownWithJPY, s.peak-s.pnl)
}

// Restrict returns the report limited to the JST days in [from, to) with the
// totals recomputed. Replay reports every day any trade touched, including
// the day an overnight position was opened before the range or closed after
// it; the CLI restricts the output to the days the user asked for.
func (r Report) Restrict(from, to time.Time) Report {
	first, end := from.In(TradingDayZone).Format(DayLayout), to.In(TradingDayZone).Format(DayLayout)
	days := make([]DayResult, 0, len(r.Days))
	for _, d := range r.Days {
		if d.Day >= first && d.Day < end {
			days = append(days, d)
		}
	}
	out := Report{Limits: r.Limits, Days: days, Limitations: r.Limitations}
	out.sum()
	return out
}

func (r *Report) sum() {
	for _, d := range r.Days {
		r.Total.TradesAll += d.TradesAll
		r.Total.TradesKept += d.TradesKept
		r.Total.TradesSkipped += d.TradesSkipped
		r.Total.PnLWithoutJPY += d.PnLWithoutJPY
		r.Total.PnLWithJPY += d.PnLWithJPY
		r.Total.PnLSkippedJPY += d.PnLSkippedJPY
		r.Total.MissingPnL += d.MissingPnL
		r.MissingPnL += d.MissingPnL
		r.Total.MaxDrawdownWithoutJPY = max(r.Total.MaxDrawdownWithoutJPY, d.MaxDrawdownWithoutJPY)
		r.Total.MaxDrawdownWithJPY = max(r.Total.MaxDrawdownWithJPY, d.MaxDrawdownWithJPY)
		if d.HaltReason != "" {
			r.HaltedDays++
		}
	}
}
