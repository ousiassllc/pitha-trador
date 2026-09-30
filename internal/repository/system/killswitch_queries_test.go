package system_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

func TestKillSwitchRepository_ListRecent_NewestTriggeredFirst(t *testing.T) {
	repo := system.NewKillSwitchRepository(newTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	first, _ := repo.Insert(ctx, domain.KillSwitchEvent{TriggeredAt: base, Reason: domain.KillReasonDailyLossLimit})
	second, _ := repo.Insert(ctx, domain.KillSwitchEvent{TriggeredAt: base.Add(time.Hour), Reason: domain.KillReasonDailyLossLimit})

	got, err := repo.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("ListRecent = %+v, want [second first]", got)
	}
	if limited, _ := repo.ListRecent(ctx, 1); len(limited) != 1 || limited[0].ID != second.ID {
		t.Fatalf("ListRecent(limit=1) = %+v, want only the newest", limited)
	}
}

func TestKillSwitchRepository_Observer_SeesInsertedRow(t *testing.T) {
	repo := system.NewKillSwitchRepository(newTestDB(t))
	var seen []domain.KillSwitchEvent
	repo.SetObserver(func(_ context.Context, ev domain.KillSwitchEvent) { seen = append(seen, ev) })

	created, err := repo.Insert(context.Background(), domain.KillSwitchEvent{Reason: domain.KillReasonDailyLossLimit})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(seen) != 1 || seen[0].ID != created.ID || seen[0].Reason != domain.KillReasonDailyLossLimit {
		t.Fatalf("observed = %+v, want the inserted row (ID %d)", seen, created.ID)
	}
}
