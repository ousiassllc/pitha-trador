package hometests

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
	"github.com/ousiassllc/pitha-trador/internal/web/settingspages"
)

var hrefRe = regexp.MustCompile(`href="(/settings[^"]*)"`)

// Every Settings link the Home screen can show opens a page the Settings area
// has and, with a `#modal-…` / update-panel anchor, a dialog that page holds, so a
// renamed page or a moved dialog breaks here rather than for the operator.
func TestSettingsLinksResolveToRealPagesAndDialogs(t *testing.T) {
	f := newFixture()
	f.state = domain.SystemStateKilled
	f.halt = lossLimitHalt()
	f.market.Issue, f.market.Guidance = "rejected", "g"
	f.unsetKeys = []string{"SLACK_WEBHOOK_URL"}
	f.update = updater.Status{Available: true, Version: "v2"}
	f.source.health.AttentionCodes = home.AttentionCodes()
	f.source.health.MetaAdopted, f.source.health.BootstrapEnabled = false, false
	f.source.rejects = []home.RejectCount{{Reason: "entry_quality_below_threshold", Count: 1}}

	var bodies []string
	for _, path := range []string{"/", "/home/trade-status", "/home/attention", "/home/details"} {
		bodies = append(bodies, f.mustGet(t, path))
	}
	seen := map[string]bool{}
	for _, m := range hrefRe.FindAllStringSubmatch(strings.Join(bodies, "\n"), -1) {
		href := m[1]
		if seen[href] {
			continue
		}
		seen[href] = true
		path, anchor, _ := strings.Cut(href, "#")
		if path == settingspages.BasePath {
			continue
		}
		page, ok := settingspages.PageByID(strings.TrimPrefix(path, settingspages.BasePath+"/"))
		if !ok {
			t.Errorf("%s: no such Settings page", href)
			continue
		}
		if anchor == "" {
			continue
		}
		if owner, ok := settingspages.PageOfTarget(anchor); !ok || owner.ID != page.ID {
			t.Errorf("%s: dialog %q is not on page %q (it is on %q)", href, anchor, page.ID, owner.ID)
		}
	}
	if len(seen) < 5 {
		t.Errorf("only %d distinct Settings links were exercised: %v", len(seen), seen)
	}
}
