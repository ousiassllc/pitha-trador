package domain

import "strings"

// Scan-list paging bounds for the Scanner Dashboard / API (issue #303).
const (
	DefaultScanPageSize = 50
	MaxScanPageSize     = 200
)

// ScanQuery filters and pages a ScanCycle's per-symbol list. The zero
// value lists page 1 of everything.
type ScanQuery struct {
	// Q is a case-insensitive substring match on symbol or name.
	Q string
	// Status keeps only symbols with this status; empty keeps all.
	Status ScanStatus
	// Reason keeps only symbols excluded for this reason; nil keeps all.
	Reason *ScreenReason
	// Page is 1-based; PageSize defaults to DefaultScanPageSize and is
	// capped at MaxScanPageSize.
	Page, PageSize int
}

// ScanPage is one page of a ScanQuery result.
type ScanPage struct {
	Symbols  []ScanSymbol // the requested window of the matches
	Total    int          // matches across all pages
	Page     int          // the (clamped) page returned, 1-based
	PageSize int
	Pages    int // number of pages, at least 1
}

// Query applies q to the cycle's symbols. Symbols keep their (symbol)
// order. A page past the end is clamped to the last page.
func (c ScanCycle) Query(q ScanQuery) ScanPage {
	size := q.PageSize
	switch {
	case size <= 0:
		size = DefaultScanPageSize
	case size > MaxScanPageSize:
		size = MaxScanPageSize
	}
	needle := strings.ToLower(strings.TrimSpace(q.Q))

	matches := make([]ScanSymbol, 0, len(c.Symbols))
	for _, s := range c.Symbols {
		if q.Reason != nil && !s.Reasons.Has(*q.Reason) {
			continue
		}
		if q.Status != "" && s.Status() != q.Status {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(s.Symbol), needle) && !strings.Contains(strings.ToLower(s.Name), needle) {
			continue
		}
		matches = append(matches, s)
	}

	pages := max(1, (len(matches)+size-1)/size)
	page := min(max(q.Page, 1), pages)
	from := (page - 1) * size
	to := min(from+size, len(matches))
	return ScanPage{Symbols: matches[from:to], Total: len(matches), Page: page, PageSize: size, Pages: pages}
}

// ScanSummary counts a cycle's symbols by status and by reason.
type ScanSummary struct {
	Passed, Excluded, Missing int
	// ByReason is indexed by ScreenReason; a symbol excluded for several
	// reasons counts once under each.
	ByReason [screenReasonCount]int
}

// Summary tallies the whole cycle (independent of any ScanQuery).
func (c ScanCycle) Summary() ScanSummary {
	var sum ScanSummary
	for _, s := range c.Symbols {
		switch s.Status() {
		case ScanStatusPassed:
			sum.Passed++
		case ScanStatusMissing:
			sum.Missing++
		default:
			sum.Excluded++
		}
		for r := ScreenReason(0); r < screenReasonCount; r++ {
			if s.Reasons.Has(r) {
				sum.ByReason[r]++
			}
		}
	}
	return sum
}
