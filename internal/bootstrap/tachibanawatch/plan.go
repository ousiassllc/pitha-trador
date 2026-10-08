package tachibanawatch

import "github.com/ousiassllc/pitha-trador/internal/domain"

// planner fills the slots of one watch list, first come first served, never
// repeating a symbol and never exceeding max.
type planner struct {
	max     int
	entries []domain.WatchListEntry
	have    map[string]bool
}

func newPlanner(max int) *planner {
	return &planner{max: max, have: make(map[string]bool)}
}

// add puts symbol into the next free slot; false once the list is full.
func (p *planner) add(symbol, origin string, indicators []string) bool {
	if len(p.entries) >= p.max {
		return false
	}
	if symbol == "" || p.have[symbol] {
		return true
	}
	p.have[symbol] = true
	p.entries = append(p.entries, domain.WatchListEntry{Symbol: symbol, Origin: origin, Indicators: indicators})
	return true
}

func (p *planner) addAll(symbols []string, origin string) {
	for _, s := range symbols {
		if !p.add(s, origin, nil) {
			return
		}
	}
}

// planScreen is the daily_screen list: the held symbols (fixed slots, as the
// kabu ranking watch does), then the operator's manual symbols, then the
// screening's picks in score order for the remaining slots.
func planScreen(max int, held, manual []string, picks []Pick) []domain.WatchListEntry {
	p := newPlanner(max)
	p.addAll(held, domain.WatchOriginHeld)
	p.addAll(manual, domain.WatchOriginManual)
	for _, pick := range picks {
		if !p.add(pick.Symbol, domain.WatchOriginScreen, pick.Indicators) {
			break
		}
	}
	return p.entries
}

// planFixed is the fixed list as the operator wrote it (at most max).
func planFixed(max int, fixed []string) []domain.WatchListEntry {
	p := newPlanner(max)
	p.addAll(fixed, domain.WatchOriginFixed)
	return p.entries
}

// planFixedFallback is the fixed list behind the held symbols (used when the
// daily bars were unusable).
func planFixedFallback(max int, held, fixed []string) []domain.WatchListEntry {
	p := newPlanner(max)
	p.addAll(held, domain.WatchOriginHeld)
	p.addAll(fixed, domain.WatchOriginFixed)
	return p.entries
}

// planCarryOver is the previous list behind the held symbols: the symbols that
// held a slot only because they were held then are dropped.
func planCarryOver(max int, held []string, previous domain.WatchList) []domain.WatchListEntry {
	p := newPlanner(max)
	p.addAll(held, domain.WatchOriginHeld)
	for _, e := range previous.Entries {
		if e.Origin == domain.WatchOriginHeld {
			continue
		}
		if !p.add(e.Symbol, e.Origin, e.Indicators) {
			break
		}
	}
	return p.entries
}
