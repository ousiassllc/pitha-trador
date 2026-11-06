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

// An h2 section heading is followed by a 24px gap (mb-6, --gap-h2) and the body
// under it carries no top margin of its own (brand-guide S2, issue #991; this
// replaces the 8px of issue #968, which was the body's own mt-2).
func TestDetail_SectionHeadingsHaveTheH2GapBelow(t *testing.T) {
	_, body := get(t, outcomesSource(t), "/order-proposals/2")
	for _, section := range []string{"order-proposal-rationale", "order-proposal-exit-plan", "order-proposal-sources", "order-proposal-events"} {
		i := strings.Index(body, `data-testid="`+section+`"`)
		if i < 0 {
			t.Fatalf("no section %s", section)
		}
		rest := body[i:]
		if end := strings.Index(rest, "</section>"); end >= 0 {
			rest = rest[:end]
		}
		if !strings.Contains(rest, `<h2 class="mb-6 text-xl`) {
			t.Errorf("%s: h2 lacks mb-6: %s", section, rest)
		}
		if strings.Contains(rest, `class="mt-2 `) || strings.Contains(rest, `class="mt-1 `) {
			t.Errorf("%s: body keeps its own top margin: %s", section, rest)
		}
	}
}
