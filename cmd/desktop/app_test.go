package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestApp_SetupNotifications_InitializesOnceAndLogsFailure regresses issue
// #549: runtime.SendNotification needs InitializeNotifications first (the
// Windows toast AppID is set there), and a failure must be logged, not fatal.
func TestApp_SetupNotifications_InitializesOnceAndLogsFailure(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	calls := 0
	app := NewApp()
	app.initNotifications = func(context.Context) error {
		calls++
		return errors.New("toast registration denied")
	}

	app.setupNotifications(context.Background()) // must not panic or exit

	if calls != 1 {
		t.Fatalf("initNotifications called %d times, want 1", calls)
	}
	out := logs.String()
	if !strings.Contains(out, "initialize native notifications failed") || !strings.Contains(out, "toast registration denied") {
		t.Errorf("log %q does not report the initialization error", out)
	}
}

func TestApp_SetupNotifications_SuccessLogsNothing(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	app := NewApp()
	app.initNotifications = func(context.Context) error { return nil }
	app.setupNotifications(context.Background())

	if logs.Len() != 0 {
		t.Errorf("unexpected log output on success: %q", logs.String())
	}
}
