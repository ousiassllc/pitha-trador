package orderproposals_test

import (
	"strings"
	"testing"
)

// The 状態の時系列 table uses the standard table recipe (padding 10 / 12, brand-guide
// §4 テーブル): the same cell padding in every column, none of the old 0 / 6 / 4 mix (issue #966).
func TestDetail_EventsTableUsesTheStandardCellPadding(t *testing.T) {
	_, body := get(t, outcomesSource(t), "/order-proposals/2")
	if n := strings.Count(body, `class="px-3 py-2.5"`); n < 5*3 {
		t.Errorf("cells with px-3 py-2.5 = %d, want every th/td of the 2 events + header (>= 15)", n)
	}
	for _, old := range []string{`class="py-1.5 pr-3"`, `class="py-1"`} {
		if strings.Contains(body, old) {
			t.Errorf("events table kept the off-recipe padding %s", old)
		}
	}
}
