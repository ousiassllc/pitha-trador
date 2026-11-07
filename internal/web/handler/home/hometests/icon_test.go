package hometests

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
)

// arrowRightPath is the one path Iconoir's arrow-right draws (atoms.IconArrowRight).
const arrowRightPath = `d="M3 12L21 12M21 12L12.5 3.5M21 12L12.5 20.5"`

var literalComponentCall = regexp.MustCompile(`@[A-Za-z_][A-Za-z0-9_]*\.[A-Z][A-Za-z0-9_]*\(`)

// Templ treats `@Component()` as a call only where an element child may start;
// after plain text it is emitted as text. Home's "go to" links once printed
// `成績 @atoms.IconArrowRight()` for that reason.
func TestHome_GoToLinksRenderTheArrowIconNotTheCallText(t *testing.T) {
	f := newFixture()
	// Every part of the screen that can carry a link: a failed-open state so
	// 取引の状態 and 要対応 have 「どこを見るか」 links, plus the always-present
	// blocks of 今日の詳細.
	f.state = domain.SystemStateKilled
	f.halt = lossLimitHalt()
	f.source.health.AttentionCodes = home.AttentionCodes()
	f.source.health.StalledProposals = 2

	pages := []string{"/", "/home/trade-status", "/home/pnl", "/home/positions", "/home/attention", "/home/details"}
	for _, path := range pages {
		body := f.mustGet(t, path)
		if m := literalComponentCall.FindString(body); m != "" {
			t.Errorf("GET %s prints the component call as text (%q)", path, m)
		}
		if strings.Contains(body, "@atoms.") {
			t.Errorf("GET %s contains the text %q", path, "@atoms.")
		}
	}

	// The five blocks of 今日の詳細 each end in a link with the arrow inside it.
	details := f.mustGet(t, "/home/details")
	for _, href := range []string{"/scanner", "/order-proposals", "/performance", "/settings/results", "/activity"} {
		link := between(details, `href="`+href+`"`, "</a>")
		if !strings.Contains(link, "<svg") || !strings.Contains(link, arrowRightPath) {
			t.Errorf("link to %s lacks the arrow-right svg: %q", href, link)
		}
		// A space separates the label from the icon; the icon must not touch it.
		if !strings.Contains(link, " <svg") {
			t.Errorf("link to %s has no space before the icon: %q", href, link)
		}
	}

	// 「どこを見るか」 links (取引の状態 card, 要対応 items) carry the arrow too.
	for _, path := range []string{"/home/trade-status", "/home/attention"} {
		body := f.mustGet(t, path)
		links := strings.Split(body, `data-testid="home-link"`)[1:]
		if len(links) == 0 {
			t.Fatalf("GET %s rendered no home-link; the fixture no longer exercises DestLink", path)
		}
		for i, rest := range links {
			link := rest[:strings.Index(rest, "</a>")]
			if !strings.Contains(link, arrowRightPath) {
				t.Errorf("GET %s link %d lacks the arrow-right svg: %q", path, i, link)
			}
			if !strings.Contains(link, " <svg") {
				t.Errorf("GET %s link %d has no space before the icon: %q", path, i, link)
			}
		}
	}
}
