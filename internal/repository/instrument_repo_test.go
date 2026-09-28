package repository_test

import (
	"context"
	"errors"
	"testing"

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
