package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestCleanupStaleUpdateDownloads_RemovesPreviousInstallerDir regresses
// issue #590: a successful self-update left its pitha-trador-update-* dir
// in the temp directory forever; the restarted app now removes it.
func TestCleanupStaleUpdateDownloads_RemovesPreviousInstallerDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	stale := filepath.Join(tmp, "pitha-trador-update-123")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	cleanupStaleUpdateDownloads(time.Now())

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dir still exists (stat err = %v), want removed", err)
	}
}
