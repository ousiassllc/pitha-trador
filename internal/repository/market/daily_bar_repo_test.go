package market_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func testBar(date string, closing float64) domain.DailyBar {
	return domain.DailyBar{Symbol: "7203", TradeDate: date, Open: closing, High: closing + 1, Low: closing - 1, Close: closing, Volume: 1000,
		AdjOpen: closing / 2, AdjHigh: (closing + 1) / 2, AdjLow: (closing - 1) / 2, AdjClose: closing / 2, AdjVolume: 2000}
}

func TestDailyBarRepository_SaveLatestList(t *testing.T) {
	ctx := context.Background()
	repo := market.NewDailyBarRepository(newTestDB(t))

	if got, err := repo.Latest(ctx, "7203"); err != nil || got != nil {
		t.Fatalf("Latest on empty = %+v, %v; want nil, nil", got, err)
	}
	n, err := repo.Save(ctx, "7203", []domain.DailyBar{testBar("2026-10-06", 100), testBar("2026-10-07", 110)}, false)
	if err != nil || n != 2 {
		t.Fatalf("Save = %d, %v; want 2, nil", n, err)
	}
	latest, err := repo.Latest(ctx, "7203")
	if err != nil || latest == nil || latest.TradeDate != "2026-10-07" || latest.Close != 110 || latest.AdjClose != 55 || latest.AdjVolume != 2000 {
		t.Fatalf("Latest = %+v, %v", latest, err)
	}

	// Saving a stored day again overwrites it instead of failing or duplicating.
	if _, err := repo.Save(ctx, "7203", []domain.DailyBar{testBar("2026-10-07", 120)}, false); err != nil {
		t.Fatalf("Save again: %v", err)
	}
	list, err := repo.ListBySymbol(ctx, "7203")
	if err != nil || len(list) != 2 || list[0].TradeDate != "2026-10-06" || list[1].Close != 120 {
		t.Fatalf("ListBySymbol = %+v, %v", list, err)
	}
}

func TestDailyBarRepository_ReplaceDropsTheOldHistory(t *testing.T) {
	ctx := context.Background()
	repo := market.NewDailyBarRepository(newTestDB(t))
	if _, err := repo.Save(ctx, "7203", []domain.DailyBar{testBar("2026-10-05", 100), testBar("2026-10-06", 100)}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, "6758", []domain.DailyBar{{Symbol: "6758", TradeDate: "2026-10-06", Open: 1, High: 1, Low: 1, Close: 1}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, "7203", []domain.DailyBar{testBar("2026-10-06", 90)}, true); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.ListBySymbol(ctx, "7203")
	if len(list) != 1 || list[0].Close != 90 {
		t.Fatalf("7203 after replace = %+v, want only the fresh bar", list)
	}
	if other, _ := repo.ListBySymbol(ctx, "6758"); len(other) != 1 {
		t.Fatalf("another symbol was touched: %+v", other)
	}
}

func TestDailyBarRepository_RejectsABadDate(t *testing.T) {
	repo := market.NewDailyBarRepository(newTestDB(t))
	if _, err := repo.Save(context.Background(), "7203", []domain.DailyBar{testBar("20261006", 100)}, false); err == nil {
		t.Fatal("Save with a non-ISO date succeeded; want the CHECK constraint to reject it")
	}
}

func TestDailyBarRunRepository_SaveGet(t *testing.T) {
	ctx := context.Background()
	repo := market.NewDailyBarRunRepository(newTestDB(t))

	if _, ok, err := repo.Get(ctx, "2026-10-08"); err != nil || ok {
		t.Fatalf("Get on empty = ok %v, %v", ok, err)
	}
	started := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	run := domain.DailyBarRun{RunDate: "2026-10-08", Status: domain.DailyBarRunRunning, StartedAt: started, Symbols: 10, Cursor: "1004"}
	if err := repo.Save(ctx, run); err != nil {
		t.Fatal(err)
	}
	finished := started.Add(time.Minute)
	run.Status, run.FinishedAt, run.Requests, run.SavedBars, run.Failed, run.DurationMS, run.Error =
		domain.DailyBarRunFailed, &finished, 4, 8, 1, 60000, "セッションがありません"
	if err := repo.Save(ctx, run); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.Get(ctx, "2026-10-08")
	if err != nil || !ok {
		t.Fatalf("Get = ok %v, %v", ok, err)
	}
	if got.Status != domain.DailyBarRunFailed || got.Requests != 4 || got.SavedBars != 8 || got.Failed != 1 || got.DurationMS != 60000 ||
		got.Cursor != "1004" || got.Error != "セッションがありません" || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) || !got.StartedAt.Equal(started) {
		t.Fatalf("Get = %+v", got)
	}
}
