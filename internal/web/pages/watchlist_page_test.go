package pages_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// The rank column is a number: its header and cells are right-aligned and the
// cells are JetBrains Mono tabular (brand-guide §4 table, T5).
func TestWatchlistPage_RankColumnIsRightAlignedMono(t *testing.T) {
	body := renderPage(t, pages.WatchlistPage(domain.WatchListView{
		Enabled: true,
		Lists: []domain.WatchList{{
			ListDate: "2026-11-06", Source: domain.WatchListDailyScreen,
			Entries: []domain.WatchListEntry{{Symbol: "7203", Origin: domain.WatchOriginScreen}},
		}},
	}))
	for _, want := range []string{
		`<th scope="col" class="px-3 py-2.5 text-right">順</th>`,
		`<td class="px-3 py-2.5 text-right font-mono tabular-nums text-mute">1</td>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("watchlist table lacks %q: %s", want, body)
		}
	}
}
