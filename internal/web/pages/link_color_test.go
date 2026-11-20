package pages_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// Text links are --accent with an --mute underline on every screen, and the
// current tab is a --surface-sel pill (brand-guide §1 / §4 タブ; issue #1004, C2).
func TestPages_LinksAreAccentAndCurrentTabIsASelectedPill(t *testing.T) {
	watchlist := renderPage(t, pages.WatchlistPage(domain.WatchListView{
		Enabled: true,
		Lists: []domain.WatchList{{
			ListDate: "2026-11-06", Source: domain.WatchListDailyScreen,
			Entries: []domain.WatchListEntry{{Symbol: "7203", Origin: domain.WatchOriginScreen}},
		}},
	}))
	list := renderPage(t, pages.OrderProposalsPage(pages.ProposalListProps{
		Rows: []pages.ProposalRow{{ID: 7, Symbol: "7203"}},
	}))
	detail := renderPage(t, pages.OrderProposalDetailPage(pages.ProposalDetailProps{
		ID: 7, Symbol: "7203",
		Sources: []pages.ProposalSource{{URL: "https://example.com/a", Href: "https://example.com/a"}},
	}))
	anchor := regexp.MustCompile(`<a class="[^"]*\bunderline\b[^"]*"`)
	for name, body := range map[string]string{"watchlist": watchlist, "order proposal list": list, "order proposal detail": detail} {
		links := anchor.FindAllString(body, -1)
		if len(links) == 0 {
			t.Errorf("%s: no underlined link found", name)
		}
		for _, l := range links {
			if !strings.Contains(l, "text-accent") {
				t.Errorf("%s: link without text-accent: %s", name, l)
			}
		}
	}
	if !strings.Contains(watchlist, `class="inline-flex min-h-10 items-center rounded-pill px-4 text-item font-semibold text-ink bg-surface-sel"`) {
		t.Errorf("the current tab must be a pill on bg-surface-sel: %s", watchlist)
	}
}
