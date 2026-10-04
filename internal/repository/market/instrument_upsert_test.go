package market_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
)

func TestInstrumentRepository_Upsert_InsertsNewAndIsIdempotent(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()
	master := []domain.Instrument{
		{Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", Sector: sectorPtr("輸送用機器")},
		{Symbol: "101", Name: "TOPIX", Market: "INDEX", Kind: domain.InstrumentKindMarketIndex},
	}

	n, err := repo.Upsert(ctx, master)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if n != 2 {
		t.Fatalf("first Upsert changed %d rows, want 2", n)
	}
	stock, err := repo.GetBySymbol(ctx, "7203")
	if err != nil {
		t.Fatalf("GetBySymbol: %v", err)
	}
	if !stock.IsActive || stock.Kind != domain.InstrumentKindStock {
		t.Fatalf("new stock = %+v, want active kind=stock", stock)
	}

	n, err = repo.Upsert(ctx, master)
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if n != 0 {
		t.Fatalf("re-running the same master changed %d rows, want 0", n)
	}
	again, err := repo.GetBySymbol(ctx, "7203")
	if err != nil {
		t.Fatalf("GetBySymbol: %v", err)
	}
	if again.ID != stock.ID || !again.UpdatedAt.Equal(stock.UpdatedAt) {
		t.Fatalf("no-op re-sync rewrote the row: before %+v, after %+v", stock, again)
	}
	all, err := repo.ListActive(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListActive = %d rows (err %v), want 2 without duplicates", len(all), err)
	}
}

func TestInstrumentRepository_Upsert_RefreshesMasterFieldsButKeepsIsActive(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()
	created, err := repo.Create(ctx, domain.Instrument{Symbol: "6758", Name: "旧名称", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	created.IsActive = false
	if _, err := repo.Update(ctx, created); err != nil {
		t.Fatalf("Update: %v", err)
	}

	n, err := repo.Upsert(ctx, []domain.Instrument{
		{Symbol: "6758", Name: "ソニーグループ", Market: "TSE Prime", Sector: sectorPtr("電気機器")},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if n != 1 {
		t.Fatalf("Upsert changed %d rows, want 1", n)
	}
	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "ソニーグループ" || got.Sector == nil || *got.Sector != "電気機器" {
		t.Fatalf("master fields not refreshed: %+v", got)
	}
	if got.IsActive {
		t.Fatalf("Upsert re-activated an operator-excluded instrument: %+v", got)
	}
}

func TestInstrumentRepository_Upsert_RollsBackWholeBatchOnError(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, []domain.Instrument{
		{Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime"},
		{Symbol: "9999", Name: "不正種別", Market: "TSE Prime", Kind: "bogus"},
	})
	if err == nil {
		t.Fatalf("Upsert with an invalid kind succeeded, want error")
	}
	if _, err := repo.GetBySymbol(ctx, "7203"); !errors.Is(err, market.ErrInstrumentNotFound) {
		t.Fatalf("GetBySymbol(7203) = %v, want ErrInstrumentNotFound (batch must roll back)", err)
	}
}
