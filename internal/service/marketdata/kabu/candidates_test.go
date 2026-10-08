package kabu

import (
	"slices"
	"testing"
)

func TestInterleave_RankByRankWithoutDuplicates(t *testing.T) {
	perType := [][]string{
		{"A1", "A2", "A3"},
		{"B1", "A1", "B3"},
		{"C1"},
	}
	got := interleave(perType)
	want := []string{"A1", "B1", "C1", "A2", "A3", "B3"}
	if !slices.Equal(got, want) {
		t.Errorf("interleave = %v, want %v", got, want)
	}
	if got := interleave(nil); len(got) != 0 {
		t.Errorf("interleave(nil) = %v, want empty", got)
	}
}
