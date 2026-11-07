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

// issue #991 (S2): the section h2 is followed by 24px (mb-6) and the dl has no top margin.
func TestWatchlistPage_SectionH2HasTheH2GapBelow(t *testing.T) {
	body := renderPage(t, pages.WatchlistPage(domain.WatchListView{
		Enabled: true,
		Lists:   []domain.WatchList{{ListDate: "2026-11-06", Source: domain.WatchListDailyScreen}},
	}))
	if !strings.Contains(body, `<h2 class="mb-6 text-2xl font-semibold text-ink">`) || !strings.Contains(body, `<dl class="grid gap-x-4`) {
		t.Errorf("h2 must be mb-6 and the dl must have no mt-2: %s", body)
	}
}

// issue #1014: the link to 設定 is body text under the heading, not inside the
// tooltip (a tooltip is non-interactive: it closes on leaving it).
func TestWatchlistPage_SettingsLinkIsOutsideTheTooltip(t *testing.T) {
	body := renderPage(t, pages.WatchlistPage(domain.WatchListView{Enabled: true}))
	link := `href="/settings/broker"`
	tooltipEnd := strings.Index(body, "</h1>")
	if i := strings.Index(body, link); i < 0 || i < tooltipEnd {
		t.Errorf("the 設定 link must follow the heading, outside the HelpMark: %s", body)
	}
}
