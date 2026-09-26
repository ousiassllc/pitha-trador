package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func TestKillSwitchRepository_InsertAndGet_UnresolvedRoundTrips(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewKillSwitchRepository(db)
	triggeredAt := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

	inserted, err := repo.Insert(context.Background(), domain.KillSwitchEvent{
		TriggeredAt: triggeredAt,
		Reason:      domain.KillReasonDailyLossLimit,
		DetailJSON:  `{"daily_loss_pct":1.2}`,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if inserted.ID == 0 {
		t.Fatalf("Insert: ID = 0, want non-zero")
	}
	if inserted.ResolvedAt != nil || inserted.ResolvedBy != nil {
		t.Fatalf("Insert: ResolvedAt/ResolvedBy = %v/%v, want nil/nil", inserted.ResolvedAt, inserted.ResolvedBy)
	}

	got, err := repo.Get(context.Background(), inserted.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Reason != domain.KillReasonDailyLossLimit {
		t.Fatalf("Get: Reason = %q, want %q", got.Reason, domain.KillReasonDailyLossLimit)
	}
	if !got.TriggeredAt.Equal(triggeredAt) {
		t.Fatalf("Get: TriggeredAt = %v, want %v", got.TriggeredAt, triggeredAt)
	}
	if got.DetailJSON != `{"daily_loss_pct":1.2}` {
		t.Fatalf("Get: DetailJSON = %q", got.DetailJSON)
	}
}

func TestKillSwitchRepository_Get_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewKillSwitchRepository(db)

	if _, err := repo.Get(context.Background(), 999); err != repository.ErrKillSwitchEventNotFound {
		t.Fatalf("Get: err = %v, want ErrKillSwitchEventNotFound", err)
	}
}

func TestKillSwitchRepository_ListUnresolved_ExcludesResolvedAndOrdersMostRecentFirst(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewKillSwitchRepository(db)
	ctx := context.Background()

	older, err := repo.Insert(ctx, domain.KillSwitchEvent{
		TriggeredAt: time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC),
		Reason:      domain.KillReasonMarketDataDown,
		DetailJSON:  `{}`,
	})
	if err != nil {
		t.Fatalf("Insert older: %v", err)
	}
	newer, err := repo.Insert(ctx, domain.KillSwitchEvent{
		TriggeredAt: time.Date(2026, 9, 27, 11, 0, 0, 0, time.UTC),
		Reason:      domain.KillReasonConsecutiveLosses,
		DetailJSON:  `{}`,
	})
	if err != nil {
		t.Fatalf("Insert newer: %v", err)
	}
	resolved, err := repo.Insert(ctx, domain.KillSwitchEvent{
		TriggeredAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
		Reason:      domain.KillReasonJevAPIDown,
		DetailJSON:  `{}`,
	})
	if err != nil {
		t.Fatalf("Insert resolved: %v", err)
	}
	resolvedAt := time.Date(2026, 9, 27, 10, 30, 0, 0, time.UTC)
	if err := repo.Resolve(ctx, resolved.ID, resolvedAt, domain.ResolvedByAuto); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	got, err := repo.ListUnresolved(ctx)
	if err != nil {
		t.Fatalf("ListUnresolved: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListUnresolved: len = %d, want 2 (got %+v)", len(got), got)
	}
	if got[0].ID != newer.ID || got[1].ID != older.ID {
		t.Fatalf("ListUnresolved: IDs = [%d, %d], want [%d, %d] (most recent first)",
			got[0].ID, got[1].ID, newer.ID, older.ID)
	}
}

func TestKillSwitchRepository_Resolve_AlreadyResolvedReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	repo := repository.NewKillSwitchRepository(db)
	ctx := context.Background()

	ev, err := repo.Insert(ctx, domain.KillSwitchEvent{
		TriggeredAt: time.Now(),
		Reason:      domain.KillReasonBrokerAPIError,
		DetailJSON:  `{}`,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := repo.Resolve(ctx, ev.ID, time.Now(), domain.ResolvedByManual); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}

	if err := repo.Resolve(ctx, ev.ID, time.Now(), domain.ResolvedByManual); err != repository.ErrKillSwitchEventNotFound {
		t.Fatalf("second Resolve: err = %v, want ErrKillSwitchEventNotFound", err)
	}

	got, err := repo.Get(ctx, ev.ID)
	if err != nil {
		t.Fatalf("Get after resolve: %v", err)
	}
	if got.ResolvedAt == nil || got.ResolvedBy == nil || *got.ResolvedBy != domain.ResolvedByManual {
		t.Fatalf("Get after resolve: ResolvedAt/ResolvedBy = %v/%v", got.ResolvedAt, got.ResolvedBy)
	}
}
