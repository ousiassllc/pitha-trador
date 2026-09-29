package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func openTestDB(t *testing.T) *repository.InstrumentRepository {
	t.Helper()
	return repository.NewInstrumentRepository(newTestDB(t))
}

func TestInstrumentRepository_CreateAndGet(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Instrument{
		Symbol:   "7203",
		Name:     "トヨタ自動車",
		Market:   "TSE Prime",
		Sector:   sectorPtr("輸送用機器"),
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected created_at/updated_at to be populated, got %+v", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Symbol != "7203" || got.Name != "トヨタ自動車" || got.Market != "TSE Prime" {
		t.Fatalf("Get(%d) = %+v, want symbol/name/market to match Create input", created.ID, got)
	}
	if got.Sector == nil || *got.Sector != "輸送用機器" {
		t.Fatalf("Get(%d).Sector = %v, want 輸送用機器", created.ID, got.Sector)
	}
}

func TestInstrumentRepository_CreateWithNilSector(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Instrument{
		Symbol:   "9999",
		Name:     "セクター未設定銘柄",
		Market:   "TSE Growth",
		Sector:   nil,
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Sector != nil {
		t.Fatalf("Get(%d).Sector = %v, want nil", created.ID, *got.Sector)
	}
}

func TestInstrumentRepository_GetBySymbol_NotFound(t *testing.T) {
	repo := openTestDB(t)

	_, err := repo.GetBySymbol(context.Background(), "0000")
	if !errors.Is(err, repository.ErrInstrumentNotFound) {
		t.Fatalf("GetBySymbol(unknown) error = %v, want ErrInstrumentNotFound", err)
	}
}

func TestInstrumentRepository_ListActive_ExcludesInactive(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, domain.Instrument{Symbol: "1111", Name: "Active One", Market: "TSE Prime", IsActive: true}); err != nil {
		t.Fatalf("Create active: %v", err)
	}
	if _, err := repo.Create(ctx, domain.Instrument{Symbol: "2222", Name: "Inactive One", Market: "TSE Prime", IsActive: false}); err != nil {
		t.Fatalf("Create inactive: %v", err)
	}

	active, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].Symbol != "1111" {
		t.Fatalf("ListActive() = %+v, want exactly the active instrument 1111", active)
	}
}

func TestInstrumentRepository_Update(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.Instrument{Symbol: "3333", Name: "旧名称", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	created.Name = "新名称"
	created.IsActive = false
	updated, err := repo.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "新名称" || updated.IsActive {
		t.Fatalf("Update() = %+v, want Name=新名称, IsActive=false", updated)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Name != "新名称" || got.IsActive {
		t.Fatalf("Get after Update = %+v, want persisted Name=新名称, IsActive=false", got)
	}
}

func TestInstrumentRepository_Update_NotFound(t *testing.T) {
	repo := openTestDB(t)

	_, err := repo.Update(context.Background(), domain.Instrument{ID: 999999, Symbol: "0000", Name: "x", Market: "x"})
	if !errors.Is(err, repository.ErrInstrumentNotFound) {
		t.Fatalf("Update(unknown id) error = %v, want ErrInstrumentNotFound", err)
	}
}

func TestInstrumentRepository_Create_DuplicateSymbolRejected(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	if _, err := repo.Create(ctx, domain.Instrument{Symbol: "4444", Name: "A", Market: "TSE Prime", IsActive: true}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := repo.Create(ctx, domain.Instrument{Symbol: "4444", Name: "B", Market: "TSE Prime", IsActive: true}); err == nil {
		t.Fatalf("second Create with duplicate symbol succeeded, want UNIQUE constraint error")
	}
}

func TestInstrumentRepository_LatestStockReturns5m(t *testing.T) {
	conn := newTestDB(t)
	instRepo := repository.NewInstrumentRepository(conn)
	repo := repository.NewSnapshotRepository(conn)
	ctx := context.Background()
	at := time.Date(2026, 9, 26, 1, 15, 0, 0, time.UTC)

	create := func(symbol, kind string, active bool) domain.Instrument {
		inst, err := instRepo.Create(ctx, domain.Instrument{Symbol: symbol, Name: symbol, Market: "TSE", Kind: kind, IsActive: active})
		if err != nil {
			t.Fatalf("Create %s: %v", symbol, err)
		}
		return inst
	}
	f := func(v float64) domain.Feature { return domain.Feature{VWAP: 1, Return5m: &v} }
	insert := func(inst domain.Instrument, ts time.Time, feat domain.Feature) {
		if _, err := repo.Insert(ctx, domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: ts, Price: 1, Feature: feat}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	a := create("A", domain.InstrumentKindStock, true)
	insert(a, at.Add(-2*time.Minute), f(0.5)) // superseded by the newer bar
	insert(a, at.Add(-1*time.Minute), f(0.01))
	insert(a, at.Add(time.Minute), f(0.9)) // after until: FR-FE-1
	b := create("B", domain.InstrumentKindStock, true)
	insert(b, at, f(-0.02))
	c := create("C", domain.InstrumentKindStock, true)
	insert(c, at, domain.Feature{VWAP: 1}) // NULL return_5m
	d := create("D", domain.InstrumentKindStock, true)
	insert(d, at.Add(-10*time.Minute), f(0.3)) // before since
	insert(create("TOPIX", domain.InstrumentKindMarketIndex, true), at, f(0.04))
	insert(create("OFF", domain.InstrumentKindStock, false), at, f(0.07))

	got, err := instRepo.LatestStockReturns5m(ctx, at.Add(-3*time.Minute), at)
	if err != nil {
		t.Fatalf("LatestStockReturns5m: %v", err)
	}
	sum := 0.0
	for _, v := range got {
		sum += v
	}
	if len(got) != 2 || sum < -0.0100001 || sum > -0.0099999 {
		t.Errorf("returns = %v, want [0.01 -0.02] (latest active-stock bar per instrument within the window)", got)
	}
}

func TestInstrumentRepository_KindDefaultsToStockAndFiltersByKind(t *testing.T) {
	repo := openTestDB(t)
	ctx := context.Background()

	stock := mustCreateInstrument(t, repo, "7203")
	if stock.Kind != domain.InstrumentKindStock {
		t.Errorf("Kind = %q, want default %q", stock.Kind, domain.InstrumentKindStock)
	}
	if _, err := repo.Create(ctx, domain.Instrument{Symbol: "TOPIX", Name: "TOPIX", Market: "TSE", Kind: domain.InstrumentKindMarketIndex, IsActive: true}); err != nil {
		t.Fatalf("Create index: %v", err)
	}

	idx, err := repo.ListActiveByKind(ctx, domain.InstrumentKindMarketIndex)
	if err != nil || len(idx) != 1 || idx[0].Symbol != "TOPIX" {
		t.Fatalf("ListActiveByKind(market_index) = %v, %v", idx, err)
	}
	stocks, _ := repo.ListActiveByKind(ctx, domain.InstrumentKindStock)
	if len(stocks) != 1 || stocks[0].Symbol != "7203" {
		t.Errorf("ListActiveByKind(stock) = %v, want only 7203", stocks)
	}

	stock.Name = "Renamed"
	updated, err := repo.Update(ctx, stock)
	if err != nil || updated.Kind != domain.InstrumentKindStock {
		t.Errorf("Update = %+v, %v; want kind preserved", updated, err)
	}
	// An empty Kind on Update keeps the stored kind.
	stock.Kind = ""
	updated, err = repo.Update(ctx, stock)
	got, _ := repo.Get(ctx, stock.ID)
	if err != nil || updated.Kind != domain.InstrumentKindStock || got.Kind != domain.InstrumentKindStock {
		t.Errorf("Update with empty Kind = %+v (stored %q), %v; want kind kept", updated, got.Kind, err)
	}
}
