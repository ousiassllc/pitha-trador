package trenddiag

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
)

// The settings view is restricted to the two research.eval keys: any other key (a secret path
// among them) is refused before SQL runs.
func TestSettingsReaderOnlyReadsTheTwoEvalKeys(t *testing.T) {
	r := settingsReader{q: nil} // a nil Q would panic on any query: the refusal must come first
	for _, key := range []string{"kabu.api_password", "research.model.enabled", "research.export.enabled", "research.eval.enabled ", "", "calibration.barrier_cost_bps"} {
		if _, _, err := r.Get(t.Context(), key); err == nil || !strings.Contains(err.Error(), "not readable") {
			t.Errorf("Get(%q) = %v, want a refusal", key, err)
		}
	}
	if !researchjobsettings.IsKey(researchjobsettings.KeyEvalEnabled) || !researchjobsettings.IsKey(researchjobsettings.KeyEvalRetentionDays) {
		t.Fatal("the keys are not research settings any more")
	}
}
