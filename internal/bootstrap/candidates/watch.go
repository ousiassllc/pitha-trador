package candidates

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Watch is the ranking-driven watch list (rankingwatch.Watchlist, FR-SCHED-9).
type Watch interface {
	Contains(symbol string) bool
}

// activeStocks returns the stocks this cycle screens: every active stock, or
// - with Refresher.Watch set - only the watched ones. A ranking that is empty
// or failed in the session leaves only the held symbols on the list, so the
// candidates of a ranking that is gone are not published as if still fresh;
// outside the session the list keeps the last in-session watch list. The
// Fast Screener funnel's universe is therefore the watched symbols (at most
// rankingwatch.MaxWatched), not every stock (FR-SCAN-3/4).
func (r *Refresher) activeStocks(ctx context.Context) ([]domain.Instrument, error) {
	actives, err := r.Instruments.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if err != nil || r.Watch == nil {
		return actives, err
	}
	watched := actives[:0:0]
	for _, inst := range actives {
		if r.Watch.Contains(inst.Symbol) {
			watched = append(watched, inst)
		}
	}
	return watched, nil
}
