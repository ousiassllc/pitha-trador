package tachibanawatch

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

// recentLists is how many lists the Watchlist screen shows.
const recentLists = 7

// ViewStore is the part of the watch list store Viewer reads.
type ViewStore interface {
	Recent(ctx context.Context, limit int) ([]domain.WatchList, error)
	AtOrBefore(ctx context.Context, date string) (domain.WatchList, bool, error)
}

// Viewer serves the Watchlist screen (web/handler/watchlist.Source) and the
// banner's fallback notice (web/handler/system.WatchNoticeSource).
type Viewer struct {
	Lists ViewStore
	// Clock defaults to the wall clock.
	Clock tachibana.Clock
}

// WatchLists implements watchlist.Source: the recent lists and the one in use.
func (v Viewer) WatchLists(ctx context.Context) (domain.WatchListView, error) {
	lists, err := v.Lists.Recent(ctx, recentLists)
	if err != nil {
		return domain.WatchListView{}, err
	}
	view := domain.WatchListView{Enabled: true, Lists: lists}
	if active, ok, err := v.Lists.AtOrBefore(ctx, SessionDate(tachibana.OrReal(v.Clock).Now())); err != nil {
		return domain.WatchListView{}, err
	} else if ok {
		view.ActiveDate = active.ListDate
	}
	return view, nil
}

// WatchNotice implements system.WatchNoticeSource: while the list of the
// current 立会日 is a stand-in for one the daily bars could not make, its
// explanation; otherwise
// "". A failed read shows nothing (the banner polls again).
func (v Viewer) WatchNotice(ctx context.Context) string {
	date := SessionDate(tachibana.OrReal(v.Clock).Now())
	active, ok, err := v.Lists.AtOrBefore(ctx, date)
	if err != nil || !ok || active.ListDate != date || !active.Fallback() {
		return ""
	}
	return active.Reason
}
