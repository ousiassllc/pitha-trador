package marketdata_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

func TestStatusTracker_UnknownSymbolIsStale(t *testing.T) {
	tracker := marketdata.NewStatusTracker()
	if !tracker.IsStale("7203") {
		t.Error("IsStale for an unrecorded symbol = false, want true")
	}
	if _, ok := tracker.Status("7203"); ok {
		t.Error("Status for an unrecorded symbol reports ok=true")
	}
}

func TestStatusTracker_MarkFreshThenStale(t *testing.T) {
	tracker := marketdata.NewStatusTracker()
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

	tracker.MarkFresh("7203", now)
	if tracker.IsStale("7203") {
		t.Fatal("IsStale after MarkFresh = true, want false")
	}

	wantErr := errors.New("kabu station api timeout")
	tracker.MarkStale("7203", wantErr)
	if !tracker.IsStale("7203") {
		t.Fatal("IsStale after MarkStale = false, want true")
	}

	status, ok := tracker.Status("7203")
	if !ok {
		t.Fatal("Status ok = false after MarkStale")
	}
	if !status.LastUpdated.Equal(now) {
		t.Errorf("LastUpdated = %v, want %v (preserved from last fresh update)", status.LastUpdated, now)
	}
	if !errors.Is(status.LastError, wantErr) {
		t.Errorf("LastError = %v, want %v", status.LastError, wantErr)
	}
}

func TestStatusTracker_MarkFreshClearsStaleAndError(t *testing.T) {
	tracker := marketdata.NewStatusTracker()
	tracker.MarkStale("7203", errors.New("boom"))
	tracker.MarkFresh("7203", time.Now())

	status, ok := tracker.Status("7203")
	if !ok {
		t.Fatal("Status ok = false")
	}
	if status.Stale {
		t.Error("Stale = true after MarkFresh")
	}
	if status.LastError != nil {
		t.Errorf("LastError = %v, want nil after MarkFresh", status.LastError)
	}
}
