package system_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

func TestRuntimeSettingsRepository_Get_MissingKeyReportsNotFound(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewRuntimeSettingsRepository(db)

	value, ok, err := repo.Get(context.Background(), "system.last_ui_heartbeat_at")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("Get: ok = true, want false")
	}
	if value != "" {
		t.Fatalf("Get: value = %q, want empty", value)
	}
}

func TestRuntimeSettingsRepository_SetThenGet_RoundTrips(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewRuntimeSettingsRepository(db)
	ctx := context.Background()

	updatedAt := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if err := repo.Set(ctx, "system.last_ui_heartbeat_at", `"2026-09-27T12:00:00Z"`, updatedAt); err != nil {
		t.Fatalf("Set: %v", err)
	}

	value, ok, err := repo.Get(ctx, "system.last_ui_heartbeat_at")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("Get: ok = false, want true")
	}
	if value != `"2026-09-27T12:00:00Z"` {
		t.Fatalf("Get: value = %q", value)
	}
}

func TestRuntimeSettingsRepository_Set_OverwritesExistingKey(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewRuntimeSettingsRepository(db)
	ctx := context.Background()

	if err := repo.Set(ctx, "system.paused", "true", time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := repo.Set(ctx, "system.paused", "false", time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("second Set: %v", err)
	}

	value, ok, err := repo.Get(ctx, "system.paused")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || value != "false" {
		t.Fatalf("Get: value = %q, ok = %v, want %q, true", value, ok, "false")
	}
}
