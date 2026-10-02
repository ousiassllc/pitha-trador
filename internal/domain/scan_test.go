package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestScreenReasons_StatusClassification(t *testing.T) {
	tests := []struct {
		name string
		in   domain.ScreenReasons
		want domain.ScanStatus
	}{
		{"empty set passes", 0, domain.ScanStatusPassed},
		{"threshold failure is excluded", domain.ScreenReasons(0).Add(domain.ScreenReasonMinPrice), domain.ScanStatusExcluded},
		{"top-N cut is excluded", domain.ScreenReasons(0).Add(domain.ScreenReasonRankedOut), domain.ScanStatusExcluded},
		{"missing data is missing", domain.ScreenReasons(0).Add(domain.ScreenReasonMissingSpread), domain.ScanStatusMissing},
		{"missing wins over a threshold failure", domain.ScreenReasons(0).Add(domain.ScreenReasonMaxSpread).Add(domain.ScreenReasonNoSnapshot), domain.ScanStatusMissing},
	}
	for _, tt := range tests {
		if got := tt.in.Status(); got != tt.want {
			t.Errorf("%s: Status() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestScreenReason_CodesAreUniqueAndRoundTrip(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range domain.AllScreenReasons() {
		if r.Code() == "" || r.Label() == "" {
			t.Fatalf("reason %d has empty code/label", r)
		}
		if seen[r.Code()] {
			t.Fatalf("duplicate code %q", r.Code())
		}
		seen[r.Code()] = true
		if got, ok := domain.ScreenReasonFromCode(r.Code()); !ok || got != r {
			t.Errorf("ScreenReasonFromCode(%q) = %v, %v; want %v", r.Code(), got, ok, r)
		}
	}
	if _, ok := domain.ScreenReasonFromCode("nope"); ok {
		t.Error("unknown code resolved")
	}
	// The threshold reason codes are the strategy.yaml keys users know.
	for _, code := range []string{"min_price", "max_price", "min_turnover_5m_jpy", "max_spread_bps", "min_volume_ratio", "min_abs_return_5m_pct", "min_realized_volatility"} {
		if !seen[code] {
			t.Errorf("missing reason code %q", code)
		}
	}
}

func TestScreenReasons_ListIsInDisplayOrder(t *testing.T) {
	s := domain.ScreenReasons(0).Add(domain.ScreenReasonMaxSpread).Add(domain.ScreenReasonMinPrice)
	got := s.List()
	if len(got) != 2 || got[0] != domain.ScreenReasonMinPrice || got[1] != domain.ScreenReasonMaxSpread {
		t.Fatalf("List() = %v", got)
	}
}

func testCycle(n int) domain.ScanCycle {
	c := domain.ScanCycle{StartedAt: time.Unix(0, 0), FinishedAt: time.Unix(2, 0)}
	for i := range n {
		s := domain.ScanSymbol{Symbol: fmt.Sprintf("%04d", i), Name: fmt.Sprintf("Corp %d", i), Market: "Prime"}
		switch i % 4 {
		case 1:
			s.Reasons = s.Reasons.Add(domain.ScreenReasonMinPrice).Add(domain.ScreenReasonMaxSpread)
		case 2:
			s.Reasons = s.Reasons.Add(domain.ScreenReasonNoSnapshot)
		}
		c.Symbols = append(c.Symbols, s)
	}
	return c
}

func TestScanCycle_Query_FiltersAndPages(t *testing.T) {
	c := testCycle(10) // statuses: 0,4,8 passed; 1,5,9 excluded; 2,6 missing; 3,7 passed
	minPrice := domain.ScreenReasonMinPrice

	tests := []struct {
		name  string
		q     domain.ScanQuery
		total int
		first string
	}{
		{"no filter", domain.ScanQuery{PageSize: 100}, 10, "0000"},
		{"status excluded", domain.ScanQuery{Status: domain.ScanStatusExcluded}, 3, "0001"},
		{"status missing", domain.ScanQuery{Status: domain.ScanStatusMissing}, 2, "0002"},
		{"status passed", domain.ScanQuery{Status: domain.ScanStatusPassed}, 5, "0000"},
		{"reason", domain.ScanQuery{Reason: &minPrice}, 3, "0001"},
		{"search code", domain.ScanQuery{Q: "0007"}, 1, "0007"},
		{"search name case-insensitive", domain.ScanQuery{Q: " corp 3 "}, 1, "0003"},
		{"combined", domain.ScanQuery{Q: "corp", Status: domain.ScanStatusExcluded, Reason: &minPrice}, 3, "0001"},
		{"no match", domain.ScanQuery{Q: "zzz"}, 0, ""},
	}
	for _, tt := range tests {
		p := c.Query(tt.q)
		if p.Total != tt.total {
			t.Errorf("%s: Total = %d, want %d", tt.name, p.Total, tt.total)
		}
		if tt.first != "" && p.Symbols[0].Symbol != tt.first {
			t.Errorf("%s: first = %q, want %q", tt.name, p.Symbols[0].Symbol, tt.first)
		}
		if p.Pages < 1 || p.Page < 1 {
			t.Errorf("%s: Pages=%d Page=%d, want >= 1", tt.name, p.Pages, p.Page)
		}
	}
}

func TestScanCycle_Query_PagingBounds(t *testing.T) {
	c := testCycle(4000)
	p := c.Query(domain.ScanQuery{})
	if p.PageSize != domain.DefaultScanPageSize || len(p.Symbols) != 50 || p.Pages != 80 || p.Total != 4000 {
		t.Fatalf("default page = %+v (len %d)", p, len(p.Symbols))
	}
	if p = c.Query(domain.ScanQuery{PageSize: 100000}); p.PageSize != domain.MaxScanPageSize || len(p.Symbols) != 200 {
		t.Fatalf("page size not capped: %d/%d", p.PageSize, len(p.Symbols))
	}
	last := c.Query(domain.ScanQuery{Page: 999})
	if last.Page != 80 || len(last.Symbols) != 50 || last.Symbols[49].Symbol != "3999" {
		t.Fatalf("past-the-end page not clamped to last: page=%d len=%d", last.Page, len(last.Symbols))
	}
	if p = c.Query(domain.ScanQuery{Page: -3}); p.Page != 1 {
		t.Fatalf("Page -3 -> %d, want 1", p.Page)
	}
	if empty := (domain.ScanCycle{}).Query(domain.ScanQuery{Page: 5}); empty.Total != 0 || empty.Page != 1 || empty.Pages != 1 || len(empty.Symbols) != 0 {
		t.Fatalf("empty cycle page = %+v", empty)
	}
}

func TestScanCycle_Summary(t *testing.T) {
	sum := testCycle(10).Summary()
	if sum.Passed != 5 || sum.Excluded != 3 || sum.Missing != 2 {
		t.Fatalf("Summary = %+v, want passed 5 / excluded 3 / missing 2", sum)
	}
	if sum.ByReason[domain.ScreenReasonMinPrice] != 3 || sum.ByReason[domain.ScreenReasonMaxSpread] != 3 || sum.ByReason[domain.ScreenReasonNoSnapshot] != 2 {
		t.Fatalf("ByReason = %v", sum.ByReason)
	}
}
