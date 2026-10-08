package market_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func testWatchList(date, source string, symbols ...string) domain.WatchList {
	l := domain.WatchList{ListDate: date, Source: source, Reason: "reason " + date, BasisDate: "2026-10-07",
		DecidedAt: time.Date(2026, 10, 7, 19, 0, 0, 0, time.UTC)}
	for _, s := range symbols {
		l.Entries = append(l.Entries, domain.WatchListEntry{Symbol: s, Origin: domain.WatchOriginScreen, Indicators: []string{"gain_rate", "volume"}})
	}
	return l
}

func TestWatchListRepository_SaveGetKeepsOrderAndIndicators(t *testing.T) {
	ctx := context.Background()
	repo := market.NewWatchListRepository(newTestDB(t))

	if _, ok, err := repo.Get(ctx, "2026-10-08"); err != nil || ok {
		t.Fatalf("Get on empty = %v, %v; want not found", ok, err)
	}
	list := testWatchList("2026-10-08", domain.WatchListDailyScreen, "9984", "7203", "6758")
	list.Entries[0].Origin, list.Entries[0].Indicators = domain.WatchOriginHeld, nil
	if err := repo.Save(ctx, list); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := repo.Get(ctx, "2026-10-08")
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	if got.Source != domain.WatchListDailyScreen || got.Reason != "reason 2026-10-08" || got.BasisDate != "2026-10-07" || !got.DecidedAt.Equal(list.DecidedAt) {
		t.Fatalf("Get = %+v", got)
	}
	if syms := got.Symbols(); len(syms) != 3 || syms[0] != "9984" || syms[1] != "7203" || syms[2] != "6758" {
		t.Fatalf("symbols = %v, want the saved order", syms)
	}
	if got.Entries[0].Origin != domain.WatchOriginHeld || len(got.Entries[0].Indicators) != 0 ||
		len(got.Entries[1].Indicators) != 2 || got.Entries[1].Indicators[1] != "volume" {
		t.Fatalf("entries = %+v", got.Entries)
	}
}

func TestWatchListRepository_SaveReplacesTheListOfTheSameDate(t *testing.T) {
	ctx := context.Background()
	repo := market.NewWatchListRepository(newTestDB(t))
	if err := repo.Save(ctx, testWatchList("2026-10-08", domain.WatchListCarriedOver, "7203", "6758", "9984")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.Save(ctx, testWatchList("2026-10-08", domain.WatchListDailyScreen, "4063")); err != nil {
		t.Fatalf("Save again: %v", err)
	}
	got, _, err := repo.Get(ctx, "2026-10-08")
	if err != nil || got.Source != domain.WatchListDailyScreen || len(got.Entries) != 1 || got.Entries[0].Symbol != "4063" {
		t.Fatalf("Get = %+v, %v; want the replacement only", got, err)
	}
}

func TestWatchListRepository_AtOrBeforeLatestRecentAndRetention(t *testing.T) {
	ctx := context.Background()
	repo := market.NewWatchListRepository(newTestDB(t))
	for _, date := range []string{"2026-07-01", "2026-10-06", "2026-10-08"} {
		if err := repo.Save(ctx, testWatchList(date, domain.WatchListFixed, "7203")); err != nil {
			t.Fatalf("Save %s: %v", date, err)
		}
	}

	if _, ok, _ := repo.Get(ctx, "2026-07-01"); ok {
		t.Error("a list older than the retention must be pruned by the next Save")
	}
	if got, ok, err := repo.AtOrBefore(ctx, "2026-10-07"); err != nil || !ok || got.ListDate != "2026-10-06" || len(got.Entries) != 1 {
		t.Errorf("AtOrBefore(10-07) = %+v, %v, %v; want 10-06", got, ok, err)
	}
	if _, ok, _ := repo.AtOrBefore(ctx, "2026-10-05"); ok {
		t.Error("AtOrBefore before the first list must find nothing")
	}
	if got, ok, err := repo.Latest(ctx); err != nil || !ok || got.ListDate != "2026-10-08" {
		t.Errorf("Latest = %+v, %v, %v; want 10-08", got, ok, err)
	}
	recent, err := repo.Recent(ctx, 5)
	if err != nil || len(recent) != 2 || recent[0].ListDate != "2026-10-08" || recent[1].ListDate != "2026-10-06" || len(recent[1].Entries) != 1 {
		t.Errorf("Recent = %+v, %v; want 10-08 then 10-06 with entries", recent, err)
	}
}
