package promotiondiag

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
)

// The kinds of a candidate that is a set of settings, not answers on the same rows as the current one.
// The checklist reads such a candidate from a tuning experiment's before / after comparison
// (experiment_compare) instead of from the candidate ledger (docs: paper-results-mcp-promotion.md §6).
const (
	// KindExperiment: id is a tuning experiment's id (a positive integer).
	KindExperiment = "experiment"
	// KindSetting: id is a setting key on the MCP's whitelist; its newest experiment is read.
	KindSetting = "setting"
	// KindExperimentBefore is the Current of such a result: the experiment's before period.
	KindExperimentBefore = "experiment_before"
)

// settingsSetKinds are the candidate kinds read from an experiment.
var settingsSetKinds = []string{KindExperiment, KindSetting}

func isSettingsSet(kind string) bool { return slices.Contains(settingsSetKinds, kind) }

// experimentID is the id of an experiment candidate (ok false for any other kind).
func (r Ref) experimentID() (int64, bool) {
	if r.Kind != KindExperiment {
		return 0, false
	}
	id, err := strconv.ParseInt(r.ID, 10, 64)
	return id, err == nil && id >= 1 && strconv.FormatInt(id, 10) == r.ID
}

// validateSettingsSet checks a candidate of kind experiment / setting. It has no current (the
// experiment's own before period is) and no horizon, USD/JPY, notional or period (the experiment has
// its own periods and its trades are in yen after trading costs; no AI cost is attributed to a trade).
func (r Request) validateSettingsSet() error {
	if r.Current != nil {
		return unsupported(ReasonCurrentFixed)
	}
	for _, a := range []struct {
		name  string
		given bool
	}{{"horizon", r.Horizon != ""}, {"usd_jpy", r.USDJPY != nil}, {"notional_jpy", r.NotionalJPY != nil}, {"from", r.From != nil}, {"to", r.To != nil}} {
		if a.given {
			return fmt.Errorf("promotiondiag: %s is not used for a candidate of kind %s or %s (the experiment has its own periods)", a.name, KindExperiment, KindSetting)
		}
	}
	c := *r.Candidate
	if c.ID == "" || len(c.ID) > ledgerdiag.MaxIDLen {
		return fmt.Errorf("promotiondiag: candidate id is 1〜%d characters", ledgerdiag.MaxIDLen)
	}
	switch c.Kind {
	case KindExperiment:
		if _, ok := c.experimentID(); !ok {
			return errors.New("promotiondiag: an experiment candidate's id is the experiment's id, a positive integer")
		}
	case KindSetting:
		if !mcpwritesettings.IsAllowed(c.ID) {
			return unsupported(ReasonSettingNotListed)
		}
	}
	return nil
}
