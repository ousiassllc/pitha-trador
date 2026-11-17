package labels_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// Every skip reason of the trial has a name, so none reaches the 成績 ＞ お試し
// tab as a code.
func TestTrialSkipReasons_CoverEveryReason(t *testing.T) {
	requireCovers(t, "trial skip reasons", labels.TrialSkipReasons, jevtrial.SkipReasons)
	if got := labels.TrialSkipReason("no_such_reason"); got != labels.Unknown {
		t.Errorf("an unknown reason = %q, want %q", got, labels.Unknown)
	}
}
