package screener_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

func TestLiveSource_CandidatesReturnsEmptyBeforeFirstSet(t *testing.T) {
	src := screener.NewLiveSource()

	items, asOf, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0 before the first Set", len(items))
	}
	if !asOf.IsZero() {
		t.Errorf("asOf = %v, want zero time before the first Set", asOf)
	}
}

func TestLiveSource_CandidatesReturnsMostRecentSet(t *testing.T) {
	src := screener.NewLiveSource()
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

	src.Set([]domain.Candidate{{Symbol: "7203"}}, now)
	items, asOf, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(items) != 1 || items[0].Symbol != "7203" {
		t.Fatalf("items = %+v, want a single 7203 candidate", items)
	}
	if !asOf.Equal(now) {
		t.Errorf("asOf = %v, want %v", asOf, now)
	}

	later := now.Add(time.Minute)
	src.Set([]domain.Candidate{{Symbol: "9984"}}, later)
	items, asOf, err = src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(items) != 1 || items[0].Symbol != "9984" {
		t.Fatalf("items = %+v, want a single 9984 candidate after the second Set", items)
	}
	if !asOf.Equal(later) {
		t.Errorf("asOf = %v, want %v", asOf, later)
	}
}

func TestLiveSource_Scan_EmptyBeforeFirstCycle(t *testing.T) {
	_, ok, err := screener.NewLiveSource().Scan(context.Background())
	if err != nil || ok {
		t.Fatalf("Scan() ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestLiveSource_Scan_RetainsLatestCycleAndFoldsScoutOutcomes(t *testing.T) {
	src := screener.NewLiveSource()
	src.Set([]domain.Candidate{{Symbol: "A"}, {Symbol: "B"}, {Symbol: "C"}}, time.Now())
	src.SetScan(domain.ScanCycle{Funnel: domain.ScanFunnel{Universe: 9, FastScreenerPassed: 3}, Symbols: []domain.ScanSymbol{{Symbol: "A"}}})

	src.RecordScout("A", domain.ScoutPassed)
	src.RecordScout("B", domain.ScoutFailed)
	src.RecordScout("C", domain.ScoutError) // error: not a verdict, not counted as evaluated
	src.RecordScout("OLD", domain.ScoutPassed)

	cycle, ok, _ := src.Scan(context.Background())
	if !ok || cycle.Funnel.Universe != 9 {
		t.Fatalf("Scan() = %+v ok=%v", cycle, ok)
	}
	if cycle.Funnel.ScoutEvaluated != 2 || cycle.Funnel.ScoutPassed != 1 {
		t.Errorf("scout funnel = %+v, want evaluated 2 / passed 1", cycle.Funnel)
	}
	if _, stale := cycle.Scout["OLD"]; stale || cycle.Scout["C"] != domain.ScoutError {
		t.Errorf("Scout map = %v", cycle.Scout)
	}

	// Scan returns a copy: mutating it cannot leak into the source, and a
	// new cycle drops the old outcomes.
	cycle.Scout["A"] = domain.ScoutFailed
	if again, _, _ := src.Scan(context.Background()); again.Scout["A"] != domain.ScoutPassed {
		t.Error("Scan() leaked its Scout map")
	}
	src.SetScan(domain.ScanCycle{})
	if next, _, _ := src.Scan(context.Background()); len(next.Scout) != 0 || next.Funnel.ScoutPassed != 0 {
		t.Errorf("new cycle kept stale scout outcomes: %+v", next)
	}
}
