package newstargets_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/newstargets"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

type fakeCandidates struct {
	items []domain.Candidate
	err   error
}

func (f fakeCandidates) Candidates(context.Context) ([]domain.Candidate, time.Time, error) {
	return f.items, time.Time{}, f.err
}

type fakePositions struct {
	items []domain.Position
	err   error
}

func (f fakePositions) ListOpen(context.Context) ([]domain.Position, error) { return f.items, f.err }

func TestSource_NewsSymbols_HeldFirstThenCandidatesWithoutDuplicates(t *testing.T) {
	src := newstargets.New(
		fakeCandidates{items: []domain.Candidate{{Symbol: "6758"}, {Symbol: "7203"}, {Symbol: "6758"}}},
		fakePositions{items: []domain.Position{{Symbol: "7203"}, {Symbol: "9984"}}},
	)
	got, err := src.NewsSymbols(context.Background())
	if err != nil {
		t.Fatalf("NewsSymbols: %v", err)
	}
	if want := []string{"7203", "9984", "6758"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NewsSymbols = %v, want %v", got, want)
	}
}

func TestSource_NewsSymbols_EmptyWithoutCandidatesOrPositions(t *testing.T) {
	got, err := newstargets.New(fakeCandidates{}, fakePositions{}).NewsSymbols(context.Background())
	if err != nil || len(got) != 0 {
		t.Errorf("NewsSymbols = %v, %v, want no symbols (never the full universe)", got, err)
	}
}

func TestSource_NewsSymbols_PropagatesSourceErrors(t *testing.T) {
	boom := errors.New("boom")
	if _, err := newstargets.New(fakeCandidates{}, fakePositions{err: boom}).NewsSymbols(context.Background()); !errors.Is(err, boom) {
		t.Errorf("positions error = %v, want %v", err, boom)
	}
	if _, err := newstargets.New(fakeCandidates{err: boom}, fakePositions{}).NewsSymbols(context.Background()); !errors.Is(err, boom) {
		t.Errorf("candidates error = %v, want %v", err, boom)
	}
}
