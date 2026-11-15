package paperentryquality_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/paperpolicysettings"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/paperentryquality"
)

// TestOverlay is the Paper overlay the backtest/proposal-evaluation replay
// applies (issue #874): only the two min_entry_quality values change, and the
// Paper source's error is surfaced.
func TestOverlay(t *testing.T) {
	live := testThresholds().Policy
	paper := &source{s: paperpolicysettings.Settings{LongMinEntryQuality: "good", ShortMinEntryQuality: "fair"}}
	got, err := paperentryquality.Overlay(context.Background(), paper, live)
	if err != nil {
		t.Fatalf("Overlay: %v", err)
	}
	want := live
	want.Long.MinEntryQuality, want.Short.MinEntryQuality = "good", "fair"
	if got != want {
		t.Errorf("Overlay = %+v, want %+v", got, want)
	}
	if live.Long.MinEntryQuality != "strong" {
		t.Errorf("Overlay changed its input: %+v", live)
	}
	if _, err := paperentryquality.Overlay(context.Background(), &source{err: errors.New("boom")}, live); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Overlay with a failing source: err = %v, want it to wrap boom", err)
	}
}
