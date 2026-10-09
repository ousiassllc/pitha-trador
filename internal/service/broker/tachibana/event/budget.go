package event

import (
	"log/slog"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// account counts one connection attempt on today's tally and logs it. Planned
// swaps are gated by swapAllowed beforehand; recovery reconnects are counted
// even past the budget.
func (f *Feed) account(reason string, symbols int) {
	now := f.clock.Now()
	day := tachibana.DayKey(now)
	f.mu.Lock()
	if f.connDay != day {
		f.connDay, f.connCount = day, 0
	}
	f.connCount++
	count, over := f.connCount, f.connCount > f.budget && f.overWarned != day
	if over {
		f.overWarned = day
	}
	f.mu.Unlock()
	slog.Info("tachibana: EVENT connecting", "reason", reason, "symbols", symbols, "connections_today", count, "budget", f.budget)
	if over {
		slog.Warn("tachibana: EVENT connections exceed the daily budget (recovery reconnects continue; planned swaps are suspended)",
			"connections_today", count, "budget", f.budget)
	}
}

// pendingAdds are the wanted symbols the live subscription lacks.
func (f *Feed) pendingAdds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var add []string
	for _, s := range f.desired {
		if !slices.Contains(f.subscribed, s) {
			add = append(add, s)
		}
	}
	return add
}

// swapAllowed reports whether the live connection should be replaced now to
// pick up new symbols: there are some, and today's budget has room.
func (f *Feed) swapAllowed() bool {
	add := f.pendingAdds()
	if len(add) == 0 {
		return false
	}
	day := tachibana.DayKey(f.clock.Now())
	f.mu.Lock()
	used := f.connCount
	if f.connDay != day {
		used = 0
	}
	suspended := used >= f.budget
	warn := suspended && f.swapWarned != day
	if warn {
		f.swapWarned = day
	}
	f.mu.Unlock()
	if suspended {
		if warn {
			slog.Warn("tachibana: EVENT watch list swap suspended: the daily connection budget is used up",
				"new_symbols", len(add), "connections_today", used, "budget", f.budget)
		}
		return false
	}
	return true
}
