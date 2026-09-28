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
