package system_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

func TestHealthAlertRepository_SyncKeepsExactlyTheGivenSetAndStampsTheCheck(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewHealthAlertRepository(db)
	ctx := context.Background()
	at := func(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, time.UTC) }
	kabu := domain.HealthAlert{Code: domain.HealthAlertKabuNotLoggedIn, Since: at(0, 5)}
	idle := domain.HealthAlert{Code: domain.HealthAlertMarketDataIdle, Since: at(0, 10)}

	if err := repo.Sync(ctx, at(0, 20), []domain.HealthAlert{kabu, idle}); err != nil {
		t.Fatalf("Sync both: %v", err)
	}
	got, err := repo.Active(ctx)
	if err != nil || len(got) != 2 || got[0].Code != kabu.Code || !got[0].Since.Equal(kabu.Since) || got[1].Code != idle.Code {
		t.Fatalf("Active = %+v, %v; want kabu then idle with their Since", got, err)
	}

	// The idle condition cleared and kabu's Since moved: the row for idle goes,
	// kabu is updated in place rather than duplicated.
	kabu.Since = at(0, 6)
	if err := repo.Sync(ctx, at(0, 21), []domain.HealthAlert{kabu}); err != nil {
		t.Fatalf("Sync kabu only: %v", err)
	}
	if got, err = repo.Active(ctx); err != nil || len(got) != 1 || got[0].Code != kabu.Code || !got[0].Since.Equal(kabu.Since) {
		t.Fatalf("Active after clear = %+v, %v; want only kabu with the new Since", got, err)
	}

	if err := repo.Sync(ctx, at(0, 22), nil); err != nil {
		t.Fatalf("Sync empty: %v", err)
	}
	if got, err = repo.Active(ctx); err != nil || len(got) != 0 {
		t.Fatalf("Active after all cleared = %+v, %v; want none", got, err)
	}
	var checkedAt string
	if err := db.QueryRowContext(ctx, `SELECT checked_at FROM health_alert_checks WHERE id = 1`).Scan(&checkedAt); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if want := "2026-10-20T00:22:00.000000000Z"; checkedAt != want {
		t.Errorf("health_alert_checks.checked_at = %q, want %q", checkedAt, want)
	}
}

func TestHealthAlertRepository_RejectsAnEmptyCode(t *testing.T) {
	repo := system.NewHealthAlertRepository(newTestDB(t))
	err := repo.Sync(context.Background(), time.Now(), []domain.HealthAlert{{Since: time.Now()}})
	if err == nil {
		t.Fatal("Sync with an empty code succeeded, want a CHECK failure")
	}
}
