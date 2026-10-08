package domain

import "time"

// Watch list sources: how a 立花 監視リスト was decided (issue #730, child of
// #726; docs/architecture/er/tables-market.md §watch_lists).
const (
	// WatchListDailyScreen is the 日足スクリーニング of the night's daily bars.
	WatchListDailyScreen = "daily_screen"
	// WatchListFixed is the operator's fixed list, used as it is.
	WatchListFixed = "fixed"
	// WatchListCarriedOver is the previous list, kept because the daily bars
	// could not be fetched (or were missing).
	WatchListCarriedOver = "carried_over"
	// WatchListFixedFallback is the fixed list (plus the held and manual
	// symbols), used because the daily bars could not be fetched and there was
	// no previous list to carry over.
	WatchListFixedFallback = "fixed_fallback"
)

// Watch list entry origins: why a symbol holds a slot.
const (
	// WatchOriginHeld is a symbol with an open position or pending order (a
	// fixed slot, the first ones).
	WatchOriginHeld = "held"
	// WatchOriginManual is a symbol the operator always adds in daily_screen mode.
	WatchOriginManual = "manual"
	// WatchOriginScreen is a symbol the screening selected.
	WatchOriginScreen = "screen"
	// WatchOriginFixed is a symbol of the operator's fixed list.
	WatchOriginFixed = "fixed"
)

// WatchListEntry is one symbol of a WatchList.
type WatchListEntry struct {
	Symbol string
	// Origin is one of the WatchOrigin* constants.
	Origin string
	// Indicators are the screening indicators (config/tachibanasource
	// Screen*) that selected the symbol, in the order they are configured;
	// empty unless Origin is WatchOriginScreen.
	Indicators []string
}

// WatchList is the 監視リスト of one 立会日: at most 120 symbols decided after
// the previous close, in subscription order (held symbols first).
type WatchList struct {
	// ListDate is the 立会日 (YYYY-MM-DD, JST) the list is for.
	ListDate string
	// Source is one of the WatchList* constants.
	Source string
	// Reason says why the list is not the planned one (a fallback) and
	// otherwise describes how it was decided; operator facing, never carries
	// prices.
	Reason string
	// BasisDate is the 立会日 of the daily bars the screening used ("" when
	// none were used).
	BasisDate string
	DecidedAt time.Time
	Entries   []WatchListEntry
}

// Symbols are the symbols of the list in order.
func (l WatchList) Symbols() []string {
	out := make([]string, len(l.Entries))
	for i, e := range l.Entries {
		out[i] = e.Symbol
	}
	return out
}

// Fallback reports whether the list replaced the planned one because the
// daily bars could not be used.
func (l WatchList) Fallback() bool {
	return l.Source == WatchListCarriedOver || l.Source == WatchListFixedFallback
}

// WatchListView is what the Watchlist screen shows: the recent lists, newest
// ListDate first, and which of them is in use now.
type WatchListView struct {
	// Enabled is false when no 立花 watch list is maintained (kabu selected).
	Enabled bool
	// ActiveDate is the ListDate in use now ("" when none is yet).
	ActiveDate string
	Lists      []WatchList
}
