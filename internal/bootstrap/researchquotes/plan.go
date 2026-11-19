package researchquotes

import (
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
)

// symbolPattern is the 銘柄コード the EVENT feed accepts (event.symbolPattern);
// a malformed code is dropped here so it never uses a slot of the cap.
var symbolPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,8}$`)

// planner decides the symbols to record. The list is sticky within a JST day:
// symbols are only ever added, in first-seen order, so the EVENT subscription
// (which changes only by reconnecting, against the day's connection budget)
// changes only when a symbol appears that was not planned yet. A new JST day
// starts from scratch; a smaller cap drops from the end.
type planner struct {
	day  string
	list []string
}

// update merges kabu's symbols first (in order), then extras, into the day's
// list: duplicates and malformed codes are dropped and the list never exceeds
// limit (itself capped at tachibanasource.MaxTachibanaWatchSymbols, the EVENT
// subscription cap). It returns the plan.
func (p *planner) update(day string, kabu, extras []string, limit int) []string {
	if day != p.day {
		p.day, p.list = day, nil
	}
	if limit <= 0 || limit > tachibanasource.MaxTachibanaWatchSymbols {
		limit = tachibanasource.MaxTachibanaWatchSymbols
	}
	if len(p.list) > limit {
		p.list = p.list[:limit]
	}
	for _, group := range [][]string{kabu, extras} {
		for _, s := range group {
			s = strings.TrimSpace(s)
			if len(p.list) >= limit {
				return p.list
			}
			if symbolPattern.MatchString(s) && !slices.Contains(p.list, s) {
				p.list = append(p.list, s)
			}
		}
	}
	return p.list
}
