package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func TestJobRepository_QueueCounts_AggregatesPerQueueAndWindowsFailures(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	// jev-scout: 2 pending, 1 running, 1 recent failure, 1 old failure.
	for range 2 {
		if _, err := repo.Enqueue(ctx, repository.JobQueueJevScout, "{}", now.Add(time.Hour)); err != nil {
			t.Fatalf("Enqueue pending: %v", err)
		}
	}
	if _, err := repo.Enqueue(ctx, repository.JobQueueJevScout, "{}", now.Add(-time.Minute)); err != nil {
		t.Fatalf("Enqueue running: %v", err)
	}
	if _, err := repo.ClaimNext(ctx, repository.JobQueueJevScout, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	for _, finishedAt := range []time.Time{now.Add(-10 * time.Minute), now.Add(-3 * time.Hour)} {
		j, err := repo.Enqueue(ctx, repository.JobQueueJevScout, "{}", now.Add(-time.Hour))
		if err != nil {
			t.Fatalf("Enqueue failing job: %v", err)
		}
		if err := repo.MarkFailed(ctx, j.ID, finishedAt, "boom"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
	}
	// analytics: one succeeded job counts toward nothing.
	ok, _ := repo.Enqueue(ctx, repository.JobQueueAnalytics, "{}", now)
	if err := repo.MarkSucceeded(ctx, ok.ID, now); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	counts, err := repo.QueueCounts(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("QueueCounts: %v", err)
	}
	got := map[string]repository.JobQueueCount{}
	for _, c := range counts {
		got[c.Queue] = c
	}
	scout := got[repository.JobQueueJevScout]
	if scout.Pending != 2 || scout.Running != 1 || scout.FailedSince != 1 {
		t.Fatalf("jev-scout counts = %+v, want pending=2 running=1 failedSince=1", scout)
	}
	if a := got[repository.JobQueueAnalytics]; a.Pending != 0 || a.Running != 0 || a.FailedSince != 0 {
		t.Fatalf("analytics counts = %+v, want all zero", a)
	}
}

func TestJobRepository_ListRecent_OrdersByLatestActivityAndFiltersQueue(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	older, _ := repo.Enqueue(ctx, repository.JobQueueMarketData, "{}", now)
	if err := repo.MarkSucceeded(ctx, older.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}
	newer, _ := repo.Enqueue(ctx, repository.JobQueueFeatureCalc, "{}", now)
	if err := repo.MarkSucceeded(ctx, newer.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	all, err := repo.ListRecent(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(all) != 2 || all[0].ID != newer.ID || all[1].ID != older.ID {
		t.Fatalf("ListRecent(all) IDs = %v, want [%d %d]", jobIDs(all), newer.ID, older.ID)
	}

	filtered, _ := repo.ListRecent(ctx, repository.JobQueueMarketData, 10)
	if len(filtered) != 1 || filtered[0].ID != older.ID {
		t.Fatalf("ListRecent(market-data) IDs = %v, want [%d]", jobIDs(filtered), older.ID)
	}

	limited, _ := repo.ListRecent(ctx, "", 1)
	if len(limited) != 1 || limited[0].ID != newer.ID {
		t.Fatalf("ListRecent(limit=1) IDs = %v, want [%d]", jobIDs(limited), newer.ID)
	}
}

func jobIDs(jobs []repository.Job) []int64 {
	ids := make([]int64, len(jobs))
	for i, j := range jobs {
		ids[i] = j.ID
	}
	return ids
}

func TestJobRepository_Observer_SeesEveryCommittedTransition(t *testing.T) {
	repo := repository.NewJobRepository(newTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	var seen []string
	repo.SetObserver(func(_ context.Context, j repository.Job) { seen = append(seen, j.Status) })

	j, _ := repo.Enqueue(ctx, repository.JobQueueRiskCheck, "{}", now)
	if _, err := repo.ClaimNext(ctx, repository.JobQueueRiskCheck, now); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if err := repo.MarkFailed(ctx, j.ID, now, "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	// A failed write (unknown id) must not notify.
	if err := repo.MarkSucceeded(ctx, 9999, now); err == nil {
		t.Fatalf("MarkSucceeded(unknown) = nil, want error")
	}

	want := []string{repository.JobStatusPending, repository.JobStatusRunning, repository.JobStatusFailed}
	if len(seen) != len(want) {
		t.Fatalf("observed statuses = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("observed statuses = %v, want %v", seen, want)
		}
	}
}

func TestDecisionRepository_ListRecent_AcrossInstrumentsFiltersByType(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	insert := func(decisionType string, at time.Time) domain.JevDecision {
		t.Helper()
		d, err := repo.Insert(ctx, domain.JevDecision{
			InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, DecisionType: decisionType,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
		})
		if err != nil {
			t.Fatalf("Insert(%s): %v", decisionType, err)
		}
		return d
	}
	scout := insert(domain.JevDecisionTypeScout, base)
	trader := insert(domain.JevDecisionTypeTrader, base.Add(time.Minute))

	all, err := repo.ListRecent(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(all) != 2 || all[0].ID != trader.ID || all[1].ID != scout.ID {
		t.Fatalf("ListRecent(all) = %+v, want [trader scout]", all)
	}

	scouts, _ := repo.ListRecent(ctx, domain.JevDecisionTypeScout, 10)
	if len(scouts) != 1 || scouts[0].ID != scout.ID {
		t.Fatalf("ListRecent(scout) = %+v, want only the scout decision", scouts)
	}
}

func TestDecisionRepository_Observer_SeesInsertedRow(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	var seen []domain.JevDecision
	repo.SetObserver(func(_ context.Context, d domain.JevDecision) { seen = append(seen, d) })

	created, err := repo.Insert(context.Background(), domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: time.Now().UTC(), DecisionType: domain.JevDecisionTypeScout,
		StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(seen) != 1 || seen[0].ID != created.ID || seen[0].Symbol != "7203" {
		t.Fatalf("observed = %+v, want the inserted row (ID %d)", seen, created.ID)
	}
}

func TestKillSwitchRepository_ListRecent_NewestTriggeredFirst(t *testing.T) {
	repo := repository.NewKillSwitchRepository(newTestDB(t))
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
	repo := repository.NewKillSwitchRepository(newTestDB(t))
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
